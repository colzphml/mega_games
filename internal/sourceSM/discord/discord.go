package discord

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
)

var log = zerolog.New(os.Stdout).With().Str("package", "discord").Timestamp().Logger()

type Client struct {
	Dg          *discordgo.Session
	Cash        []string
	ChannelId   string
	HistoryDeep int
}

func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	dg, err := discordgo.New("Bot " + cfg.App.Source.Token)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Discord session")
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)

	client := &Client{
		Dg:          dg,
		Cash:        make([]string, 0),
		ChannelId:   cfg.App.Source.ChannelID,
		HistoryDeep: cfg.App.DeepHistory,
	}
	return client, nil
}

func (c *Client) Close() error {
	log.Info().Msg("Closing Discord client")
	return c.Dg.Close()
}

func (c *Client) processMessageEmbeds(message *discordgo.Message, messagesChan chan<- string, firstFlag bool) {
	if len(message.Embeds) == 0 {
		return
	}
	flag := false
	for _, embed := range message.Embeds {
		if embed.Fields == nil {
			continue
		}
		for _, field := range embed.Fields {
			if !strings.Contains(field.Value, "**") || !strings.Contains(field.Value, "MEGA/games") {
				continue
			}
			url, err := extractURL(field.Value)
			if err != nil {
				log.Error().Err(err).Msg("Error extracting URL from message")
				continue
			}
			for _, cashedURL := range c.Cash {
				if cashedURL == url {
					flag = true
					break
				}
			}
			if !flag {
				if !firstFlag {
					messagesChan <- url
				}
				c.Cash = append(c.Cash, url)
				if len(c.Cash) > 50 {
					c.Cash = c.Cash[1:]
				}
				flag = false
			}
		}
	}
}

func (c *Client) ReadLastMessages(ctx context.Context, messagesChan chan<- string, firstFlag bool) {
	time.Sleep(5 * time.Second)
	messages, err := c.Dg.ChannelMessages(c.ChannelId, c.HistoryDeep, "", "", "")
	if err != nil {
		log.Error().Err(err).Msg("Error fetching previous messages")
		return
	}

	for _, m := range messages {
		c.processMessageEmbeds(m, messagesChan, firstFlag)
	}
}

func extractURL(fieldValue string) (string, error) {
	start := strings.LastIndex(fieldValue, "(") + 1
	end := strings.LastIndex(fieldValue, ")")
	if start <= 0 || end <= start {
		return "", fmt.Errorf("URL not found")
	}
	return fieldValue[start:end], nil
}

func (c *Client) ReadMessages(ctx context.Context, wg *sync.WaitGroup, messagesChan chan<- string) {
	defer wg.Done()

	c.ReadLastMessages(ctx, messagesChan, true)

	log.Info().Msg("Discord message listener started")
	messageHandler := func(s *discordgo.Session, m *discordgo.MessageCreate) {
		c.ReadLastMessages(ctx, messagesChan, false)
	}

	c.Dg.AddHandler(messageHandler)

	if err := c.Dg.Open(); err != nil {
		log.Error().Err(err).Msg("Error opening connection to Discord")
		return
	}

	<-ctx.Done()
	c.Dg.Close()
	log.Info().Msg("Stopping Discord message listener...")
}

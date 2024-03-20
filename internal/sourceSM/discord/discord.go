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

func (c *Client) processMessageEmbeds(message *discordgo.Message, messagesChan chan<- string, processInitialMessages bool) {
	if len(message.Embeds) == 0 {
		return
	}

	for _, embed := range message.Embeds {
		if embed.Fields == nil {
			continue
		}

		for _, field := range embed.Fields {
			// Optimized check for the substring that applies to both cases
			if strings.Contains(field.Value, "MEGA") {
				switch {
				case strings.Contains(field.Value, "**") && strings.Contains(field.Value, "/games"):
					url, err := extractURL(field.Value)
					if err != nil {
						log.Error().Err(err).Msg("Error extracting URL from message")
						continue
					}

					// Check if URL is already cached
					if !contains(c.Cash, url) {
						if !processInitialMessages {
							messagesChan <- url
						}
						c.Cash = append(c.Cash, url)

						// Evict the oldest URL if the cache exceeds 50 entries
						if len(c.Cash) > 50 {
							c.Cash = c.Cash[1:]
						}
					}

				case strings.Contains(field.Value, "has advanced to"):
					// Specific logic for the second case
					log.Info().Msg("MEGA has advanced to: " + field.Value)
					// if !processInitialMessages {
					// 	log.Info().Msg("MEGA has advanced to: " + field.Value)
					// 	//messagesChan <- "MEGA has advanced to: " + field.Value
					// }
				}
			}
		}
	}
}

// Helper function to check if the slice contains a string
func contains(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
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

	c.ReadLastMessages(ctx, messagesChan, false)

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

package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type Client struct {
	session   *discordgo.Session
	channelID string
	out       chan<- string
	log       zerolog.Logger
}

func NewClient(token, channelID string, out chan<- string, log zerolog.Logger) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	return &Client{
		session:   session,
		channelID: channelID,
		out:       out,
		log:       log,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	c.session.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)

	c.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m == nil || m.Message == nil {
			return
		}
		if c.channelID != "" && m.ChannelID != c.channelID {
			return
		}

		select {
		case c.out <- m.ID:
			c.log.Info().Str("message_id", m.ID).Msg("received discord message")
		default:
			c.log.Warn().Str("message_id", m.ID).Msg("message buffer full, dropping")
		}
	})

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = c.session.Close()
	}()

	return nil
}

func (c *Client) Close() error {
	return c.session.Close()
}

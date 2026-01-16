package discord

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type Client struct {
	session   *discordgo.Session
	channelID string
	log       zerolog.Logger
	healthy   atomic.Bool
}

func NewClient(token, channelID string, log zerolog.Logger) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	return &Client{
		session:   session,
		channelID: channelID,
		log:       log,
	}, nil
}

func (c *Client) StartHealth(ctx context.Context, interval time.Duration) {
	check := func() {
		ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := c.checkHealth(ctxTimeout); err != nil {
			c.healthy.Store(false)
			c.log.Error().Err(err).Msg("discord health check failed")
			return
		}
		c.healthy.Store(true)
	}

	check()

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				c.healthy.Store(false)
				return
			case <-ticker.C:
				check()
			}
		}
	}()
}

func (c *Client) checkHealth(ctx context.Context) error {
	if _, err := c.session.User("@me", discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("discord api ping failed: %w", err)
	}
	return nil
}

func (c *Client) FetchMessage(ctx context.Context, messageID string) (*discordgo.Message, error) {
	msg, err := c.session.ChannelMessage(c.channelID, messageID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("fetch discord message: %w", err)
	}
	return msg, nil
}

func (c *Client) Healthy() bool {
	return c.healthy.Load()
}

func (c *Client) Close() error {
	return c.session.Close()
}

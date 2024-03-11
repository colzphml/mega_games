package telegram

import (
	"context"
	"os"

	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
)

var log = zerolog.New(os.Stdout).With().Str("package", "telegram").Timestamp().Logger()

type Client struct {
	ClientType string
}

func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	result := &Client{
		ClientType: "telegram",
	}
	return result, nil
}

func (c *Client) Close() {
	log.Info().Msg("closing telegram client")
}

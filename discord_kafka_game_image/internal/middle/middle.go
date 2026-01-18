package middle

import (
	"context"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/middle/gochrome"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/middle/headless"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/middle/selenium"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/types"
)

type Fetcher interface {
	Fetch(ctx context.Context, gameID string) (types.Result, error)
	Close() error
}

func NewFetcher(ctx context.Context, cfg config.Config) (Fetcher, error) {
	switch cfg.FetcherType {
	case "selenium":
		return selenium.NewClient(ctx, cfg)
	case "gochrome":
		return gochrome.NewClient(ctx, cfg)
	case "headless":
		return headless.NewClient(ctx, cfg)
	default:
		return nil, &ErrUnsupportedFetcherType{FetcherType: cfg.FetcherType}
	}
}

type ErrUnsupportedFetcherType struct {
	FetcherType string
}

func (e *ErrUnsupportedFetcherType) Error() string {
	return "unsupported fetcher type: " + e.FetcherType
}

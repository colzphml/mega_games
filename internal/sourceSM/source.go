package source

import (
	"context"
	"sync"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/internal/sourceSM/discord"
	"github.com/colzphml/mega_games/pkg/config"
)

type Sourcer interface {
	HandleMessages(ctx context.Context, wg *sync.WaitGroup)
	ProceedMessages(ctx context.Context, wg *sync.WaitGroup)
	Close() error
}

func NewSource(ctx context.Context, cfg *config.Config, messageChan chan<- model.DiscordGame, targetChan chan<- model.TargetMessage) (Sourcer, error) {
	switch cfg.App.Source.Type {
	case "discord":
		return discord.NewClient(ctx, cfg, messageChan, targetChan)
	default:
		return nil, &ErrUnsupportedSourceType{SourceType: cfg.App.Source.Type}
	}
}

// ErrUnsupportedDatabase is an error returned when an unsupported database type is requested.
type ErrUnsupportedSourceType struct {
	SourceType string
}

func (e *ErrUnsupportedSourceType) Error() string {
	return "unsupported source type: " + e.SourceType
}

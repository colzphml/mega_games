package source

import (
	"context"
	"sync"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/internal/sourceSM/discord"
	"github.com/colzphml/mega_games/pkg/config"
)

type Sourcer interface {
	ReadMessages(ctx context.Context, wg *sync.WaitGroup, messagesChan chan<- string, targetChan chan<- model.TargetMessage)
	Close() error
}

func NewSource(ctx context.Context, cfg *config.Config) (Sourcer, error) {
	switch cfg.App.Source.Type {
	case "discord":
		return discord.NewClient(ctx, cfg)
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

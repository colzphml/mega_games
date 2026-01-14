package middle

import (
	"context"
	"sync"

	"github.com/colzphml/mega_games/internal/middle/gochrome"
	"github.com/colzphml/mega_games/internal/middle/headless"
	"github.com/colzphml/mega_games/internal/middle/selenium"
	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
)

type Middler interface {
	ReadMessages(ctx context.Context, wg *sync.WaitGroup)
	Close() error
}

func NewMiddler(ctx context.Context, cfg *config.Config, messageChan <-chan model.DiscordGame, targetChan chan<- model.TargetMessage) (Middler, error) {
	switch cfg.App.Middle.Type {
	case "selenium":
		return selenium.NewClient(ctx, cfg, messageChan, targetChan)
	case "headless":
		return headless.NewClient(ctx, cfg, messageChan, targetChan)
	case "gochrome":
		return gochrome.NewClient(ctx, cfg, messageChan, targetChan)
	default:
		return nil, &ErrUnsupportedMiddleType{SourceType: cfg.App.Middle.Type}
	}
}

// ErrUnsupportedMiddleType is an error returned when an unsupported database type is requested.
type ErrUnsupportedMiddleType struct {
	SourceType string
}

func (e *ErrUnsupportedMiddleType) Error() string {
	return "unsupported source type: " + e.SourceType
}

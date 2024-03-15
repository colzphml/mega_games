package middle

import (
	"context"
	"sync"

	"github.com/colzphml/mega_games/internal/middle/selenium"
	"github.com/colzphml/mega_games/pkg/config"
)

type Middler interface {
	ReadMessages(ctx context.Context, wg *sync.WaitGroup, messagesChan <-chan string)
	Close() error
}

func NewMiddler(ctx context.Context, cfg *config.Config) (Middler, error) {
	switch cfg.App.Middle.Type {
	case "selenium":
		return selenium.NewClient(ctx, cfg)
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

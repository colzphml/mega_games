package target

import (
	"context"
	"sync"

	"github.com/colzphml/mega_games/internal/targetSM/telegram"
	"github.com/colzphml/mega_games/pkg/config"
)

type Targeter interface {
	ProceedFiles(ctx context.Context, wg *sync.WaitGroup)
	Close()
}

func NewSource(ctx context.Context, cfg *config.Config) (Targeter, error) {
	switch cfg.App.Target.Type {
	case "telegram":
		return telegram.NewClient(ctx, cfg)
	default:
		return nil, &ErrUnsupportedTargetType{TargetType: cfg.App.Source.Type}
	}
}

// ErrUnsupportedDatabase is an error returned when an unsupported database type is requested.
type ErrUnsupportedTargetType struct {
	TargetType string
}

func (e *ErrUnsupportedTargetType) Error() string {
	return "unsupported target type: " + e.TargetType
}

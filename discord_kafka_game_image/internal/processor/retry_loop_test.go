package processor

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
)

// retryLoop must never panic for a non-positive interval: time.NewTicker
// panics for d <= 0, and retryLoop runs in an unrecovered goroutine started
// from Run, so a panic here takes down the whole process at startup, before
// it has done anything. This calls retryLoop directly (not through Run,
// which needs a live store and Kafka reader) so the test stays a plain,
// fast unit test with no external dependencies.
func TestRetryLoopDoesNotPanicForNonPositiveInterval(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, -time.Minute} {
		t.Run(interval.String(), func(t *testing.T) {
			p := &Processor{
				cfg: config.Config{ProcessRetryInterval: interval},
				log: zerolog.Nop(),
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("retryLoop panicked for interval %s: %v", interval, r)
				}
			}()

			// Already-canceled: once retryLoop gets past constructing the
			// ticker, it must return via ctx.Done() without touching
			// Postgres, keeping this test dependency-free either way.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			p.retryLoop(ctx)
		})
	}
}

// A negative PROCESS_RETRY_INTERVAL has no meaningful interpretation (unlike
// zero, which deliberately disables the retry loop), so it is almost
// certainly a typo. Run rejects it outright instead of silently treating it
// like zero, so the mistake is visible at startup instead of hiding as
// "retries just never happen".
func TestRunRejectsNegativeRetryInterval(t *testing.T) {
	p := &Processor{
		cfg: config.Config{ProcessRetryInterval: -time.Minute},
		log: zerolog.Nop(),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Run panicked instead of cleanly rejecting a negative interval: %v", r)
		}
	}()

	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected Run to reject a negative PROCESS_RETRY_INTERVAL, got nil error")
	}
}

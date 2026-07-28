package store

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/pgtest"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	pool := pgtest.NewPostgres(t)
	s := &Store{pool: pool, log: zerolog.Nop()}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return s
}

func TestInFlightMessageIsNotRetried(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-1", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}

	// Отправка началась: сообщение переходит в in_progress.
	if err := s.TouchAttempt(ctx, "msg-1", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}

	// Ретрай-луп срабатывает, пока отправка ещё идёт.
	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending, want 0: an in-flight send must not be "+
			"retried, otherwise the post goes to Telegram twice", len(pending))
	}
}

func TestStuckMessageIsRetriedEventually(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-2", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-2", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}

	// Сдвигаем last_attempt_at за порог устаревания (3 x интервал).
	if _, err := s.pool.Exec(ctx,
		`UPDATE telegram_week_status SET last_attempt_at = NOW() - INTERVAL '10 minutes'
		 WHERE message_id = $1`, "msg-2"); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1: a send stuck past the threshold "+
			"must be retried", len(pending))
	}
}

func TestTouchAttemptRejectsInFlightMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-3", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-3", retryInterval); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-3", retryInterval); err == nil {
		t.Error("second TouchAttempt must fail while the first is in flight")
	}
}

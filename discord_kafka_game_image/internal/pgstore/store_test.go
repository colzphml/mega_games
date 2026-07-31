package pgstore

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

func TestListPendingReturnsNewMessages(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m1", []byte(`{"game_id":"1"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("got %#v, want one message m1", got)
	}
}

func TestListPendingSkipsProcessed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m2", []byte(`{"game_id":"2"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := s.MarkProcessed(ctx, "m2"); err != nil {
		t.Fatalf("mark processed: %v", err)
	}

	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d, want 0: processed messages must not be reprocessed", len(got))
	}
}

func TestListPendingReclaimsStuckInProgress(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m3", []byte(`{"game_id":"3"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := s.TouchAttempt(ctx, "m3", time.Minute); err != nil {
		t.Fatalf("touch: %v", err)
	}

	// Свежий in_progress не должен подхватываться.
	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0: an in-flight message must not be reclaimed", len(got))
	}

	// Застрявший — должен.
	if _, err := s.pool.Exec(ctx,
		`UPDATE game_image_status SET last_attempt_at = NOW() - INTERVAL '10 minutes' WHERE message_id = $1`,
		"m3"); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	got, err = s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1: a message stuck in progress must be reclaimed", len(got))
	}
}

// TestMoveToFailedSkipsAlreadyProcessedMessage guards the same
// customer-visible invariant as the identical test in
// discord_kafka_processor/week_formatter: a message already marked
// processed must survive MoveToFailed untouched, not be deleted from
// game_image_status and re-filed into game_image_failed.
func TestMoveToFailedSkipsAlreadyProcessedMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "msg-done", []byte(`{"game_id":"done"}`)); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.MarkProcessed(ctx, "msg-done"); err != nil {
		t.Fatalf("mark processed: %v", err)
	}

	// A concurrent attempt that lost the race finally exhausts its
	// retries and tries to fail the message out, after the winner
	// already marked it processed.
	if err := s.MoveToFailed(ctx, "msg-done", map[string]any{"reason": "late failure"}); err != nil {
		t.Fatalf("move to failed: %v", err)
	}

	msg, err := s.getMessage(ctx, "msg-done")
	if err != nil {
		t.Fatalf("get message: %v -- the row must still exist in "+
			"game_image_status, not be deleted", err)
	}
	if msg.Status != statusProcessed {
		t.Errorf("status = %q, want %q: MoveToFailed must not change the "+
			"status of an already-processed message", msg.Status, statusProcessed)
	}

	var failedCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game_image_failed WHERE message_id = $1`, "msg-done").Scan(&failedCount); err != nil {
		t.Fatalf("count failed rows: %v", err)
	}
	if failedCount != 0 {
		t.Errorf("game_image_failed has %d rows for an already-processed "+
			"message, want 0: this is what would shrink game_image_status "+
			"below the row count history is expected to keep", failedCount)
	}
}

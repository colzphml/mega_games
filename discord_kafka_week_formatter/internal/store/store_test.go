package store

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/pgtest"
	"github.com/colzphml/mega_games/internal/common/queue"
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

// TestMoveToFailedSkipsAlreadyProcessedMessage guards against the
// customer-visible symptom: week_message_status must keep exactly the row
// count the migration verifies. If one concurrent attempt already marked a
// message processed, a second, losing attempt that later exhausts its
// retry budget must not delete that row and re-file it as failed.
func TestMoveToFailedSkipsAlreadyProcessedMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	payload := WeekPayload{Season: "regular", Week: 3}

	if _, err := s.EnsureWeekMessage(ctx, "msg-done", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
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

	msg, err := s.getWeekMessage(ctx, "msg-done")
	if err != nil {
		t.Fatalf("get week message: %v -- the row must still exist in "+
			"week_message_status, not be deleted", err)
	}
	if msg.Status != StatusProcessed() {
		t.Errorf("status = %q, want %q: MoveToFailed must not change the "+
			"status of an already-processed message", msg.Status, StatusProcessed())
	}

	var failedCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM week_message_failed WHERE message_id = $1`, "msg-done").Scan(&failedCount); err != nil {
		t.Fatalf("count failed rows: %v", err)
	}
	if failedCount != 0 {
		t.Errorf("week_message_failed has %d rows for an already-processed "+
			"message, want 0: this is what shrinks week_message_status "+
			"below its migration-verified row count", failedCount)
	}
}

// TestSecondClaimOnNeverAttemptedMessageIsRejected is the core race from
// the bug report: while a message is processed for the first time,
// last_attempt_at is still NULL, which bypasses the staleness threshold
// entirely no matter how large it is. Without a claim step here, nothing
// stops a second worker from picking up the same never-attempted message
// and processing it a second time.
func TestSecondClaimOnNeverAttemptedMessageIsRejected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	payload := WeekPayload{Season: "regular", Week: 1}

	if _, err := s.EnsureWeekMessage(ctx, "msg-1", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
	}

	if err := s.TouchAttempt(ctx, "msg-1", retryInterval); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-1", retryInterval); err == nil {
		t.Error("a second claim on an in-flight, never-before-attempted " +
			"week message must be rejected, otherwise the post goes out twice")
	}
}

func TestInFlightMessageIsNotRelisted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	payload := WeekPayload{Season: "regular", Week: 2}

	if _, err := s.EnsureWeekMessage(ctx, "msg-2", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-2", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}

	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending, want 0: an in-flight claim must not be relisted", len(pending))
	}
}

func TestStuckMessageIsRetriedEventually(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	payload := WeekPayload{Season: "regular", Week: 4}

	if _, err := s.EnsureWeekMessage(ctx, "msg-3", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-3", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE week_message_status SET last_attempt_at = NOW() - INTERVAL '10 minutes' WHERE message_id = $1`,
		"msg-3"); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1: a claim stuck past the threshold must be retried", len(pending))
	}
}

// --- Exact-boundary tests ---
//
// These pin last_attempt_at to precisely queue.StaleCutoff(now, interval)
// -- not "just now", not "long ago" -- by computing the cutoff once in the
// test and reusing that exact time.Time value both for the write and (via
// the now-injected *At helpers) for the query. A non-strict <= at the SQL
// level would make both of these tests observe the row as eligible; only
// a strict < excludes a claim exactly at the boundary. This is the same
// off-by-one that used to cause duplicate Telegram posts (V5-07) -- a
// send taking exactly one threshold's worth of time was reclaimed while
// still in flight, and a reviewer flagged this exact non-strict
// comparison still being present in this service.

func TestListPendingExcludesStuckClaimExactlyAtThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	payload := WeekPayload{Season: "regular", Week: 5}

	if _, err := s.EnsureWeekMessage(ctx, "msg-boundary-list", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
	}

	now := time.Now()
	cutoff := queue.StaleCutoff(now, retryInterval)
	if _, err := s.pool.Exec(ctx,
		`UPDATE week_message_status SET status = $1, last_attempt_at = $2 WHERE message_id = $3`,
		StatusInProgress(), cutoff, "msg-boundary-list"); err != nil {
		t.Fatalf("pin row to the threshold boundary: %v", err)
	}

	pending, err := s.listPendingAt(ctx, 100, retryInterval, now)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending, want 0: a claim exactly at the staleness "+
			"threshold has not yet gone stale -- only strictly older claims "+
			"may be retried", len(pending))
	}
}

func TestTouchAttemptRejectsClaimExactlyAtThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	payload := WeekPayload{Season: "regular", Week: 6}

	if _, err := s.EnsureWeekMessage(ctx, "msg-boundary-touch", payload); err != nil {
		t.Fatalf("ensure week message: %v", err)
	}

	now := time.Now()
	cutoff := queue.StaleCutoff(now, retryInterval)
	if _, err := s.pool.Exec(ctx,
		`UPDATE week_message_status SET status = $1, last_attempt_at = $2 WHERE message_id = $3`,
		StatusInProgress(), cutoff, "msg-boundary-touch"); err != nil {
		t.Fatalf("pin row to the threshold boundary: %v", err)
	}

	if err := s.touchAttemptAt(ctx, "msg-boundary-touch", retryInterval, now); err == nil {
		t.Error("a claim exactly at the staleness threshold must not be " +
			"reclaimable yet -- only strictly older claims may be stolen")
	}
}

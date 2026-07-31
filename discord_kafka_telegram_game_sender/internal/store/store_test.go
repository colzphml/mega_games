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

// TestRecordAttemptDoesNotResurrectAlreadyProcessedMessage reproduces the
// full seven-step sequence from the bug report. No two calls happen
// concurrently -- each step below is a single, ordinary store call, run in
// the exact order it happens on a live host:
//
//  1. Worker A claims the message (TouchAttempt: new -> in_progress).
//  2. A's send hangs past the staleness threshold (nothing to simulate
//     here beyond making last_attempt_at look old in step 3).
//  3. The retry loop finds the claim stale and reclaims it for worker B --
//     the correct, intended crash-recovery path, not a bug. Simulated by
//     backdating last_attempt_at and calling TouchAttempt again, exactly
//     as retryLoop -> reprocessPending -> processMessage would.
//  4. Worker B (the reclaiming attempt) sends successfully and marks the
//     message processed.
//  5. Only now does A return -- with an error -- and calls RecordAttempt,
//     unaware it was ever reclaimed.
//  6. On the original code, RecordAttempt unconditionally sets status
//     back to "new", overwriting B's "processed" even though B's send
//     already succeeded.
//  7. The next ordinary tick would then claim the "new" row with no
//     contention at all and send the game to Telegram a second time.
//
// This test is red on the original RecordAttempt (which sets
// `status = $1` unconditionally) because after step 5 the status reads
// "new" instead of staying "processed", and a subsequent claim succeeds
// when it must not. It is green once RecordAttempt stops touching status,
// matching the four sibling services that never had this bug.
func TestRecordAttemptDoesNotResurrectAlreadyProcessedMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	const id = "msg-race"

	if _, err := s.EnsureMessage(ctx, id, `{"game_url":"https://example.com/game"}`); err != nil {
		t.Fatalf("ensure message: %v", err)
	}

	// Step 1: worker A claims the message.
	if err := s.TouchAttempt(ctx, id, retryInterval); err != nil {
		t.Fatalf("worker A claim: %v", err)
	}

	// Steps 2-3: A's send hangs past the staleness threshold; the retry
	// loop finds the claim stale and reclaims it for worker B.
	if _, err := s.pool.Exec(ctx,
		`UPDATE telegram_game_status SET last_attempt_at = NOW() - INTERVAL '10 minutes' WHERE message_id = $1`,
		id); err != nil {
		t.Fatalf("age the row to simulate A hanging: %v", err)
	}
	if err := s.TouchAttempt(ctx, id, retryInterval); err != nil {
		t.Fatalf("worker B reclaim: %v", err)
	}

	// Step 4: B sends successfully and marks the message processed.
	if err := s.MarkProcessed(ctx, id); err != nil {
		t.Fatalf("worker B marks processed: %v", err)
	}

	// Step 5: A finally returns, with an error, unaware it was reclaimed.
	if _, err := s.RecordAttempt(ctx, id, "worker A: context deadline exceeded"); err != nil {
		t.Fatalf("worker A record attempt: %v", err)
	}

	// Step 6: the message must still read "processed" -- A's belated
	// failure must not undo B's already-delivered send.
	msg, err := s.getMessage(ctx, id)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if msg.Status != statusProcessed {
		t.Fatalf("status = %q after A's belated failed attempt, want %q: "+
			"a losing worker's late error must not resurrect an "+
			"already-delivered message back to new", msg.Status, statusProcessed)
	}

	// Step 7: prove the "next ordinary tick" really cannot re-send it --
	// neither an uncontended claim nor a retry-loop listing may pick this
	// message up again.
	if err := s.TouchAttempt(ctx, id, retryInterval); err == nil {
		t.Error("TouchAttempt succeeded on an already-processed message: " +
			"the next tick would re-send the same game to Telegram")
	}
	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, m := range pending {
		if m.ID == id {
			t.Fatalf("ListPending returned %q: an already-processed message must never be a retry candidate", id)
		}
	}
}

// TestFailedAttemptIsRetriedOnceStale is the safety net for the fix above:
// a message whose attempt genuinely failed -- no other worker ever
// touched it -- must still come back for retry once its claim goes stale,
// exactly like a crashed worker's claim would. This is expected to pass
// both before and after the RecordAttempt fix: ListPending already
// reclaims a stale in_progress row (queue.StaleCutoff), so retry-after-
// failure was never actually dependent on RecordAttempt resetting status
// to "new" -- it only ever needed last_attempt_at to age past the
// threshold, which RecordAttempt has always set correctly regardless of
// what it does with status.
func TestFailedAttemptIsRetriedOnceStale(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute
	const id = "msg-retry"

	if _, err := s.EnsureMessage(ctx, id, `{"game_url":"https://example.com/game"}`); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.TouchAttempt(ctx, id, retryInterval); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := s.RecordAttempt(ctx, id, "temporary network error"); err != nil {
		t.Fatalf("record attempt: %v", err)
	}

	// Immediately after the failure the message must not be retried yet
	// -- it only just failed, it has not gone stale.
	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending immediately after a failed attempt, want 0: "+
			"a fresh failure must wait out the staleness window like any other claim", len(pending))
	}

	// Once the failed claim goes stale, it must become eligible again --
	// otherwise a message that fails even once would be stuck forever.
	if _, err := s.pool.Exec(ctx,
		`UPDATE telegram_game_status SET last_attempt_at = NOW() - INTERVAL '10 minutes' WHERE message_id = $1`,
		id); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	pending, err = s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("got %#v, want exactly one pending message %q: a message that "+
			"genuinely failed must become retryable again once its claim is stale", pending, id)
	}
	if err := s.TouchAttempt(ctx, id, retryInterval); err != nil {
		t.Fatalf("re-claim after genuine failure: %v", err)
	}
}

// TestMoveToFailedSkipsAlreadyProcessedMessage guards the same
// customer-visible invariant as the identical test in
// discord_kafka_processor/week_formatter: a message already marked
// processed must survive MoveToFailed untouched, not be deleted from
// telegram_game_status and re-filed into telegram_game_failed.
func TestMoveToFailedSkipsAlreadyProcessedMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "msg-done", `{"game_url":"https://example.com/game"}`); err != nil {
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
			"telegram_game_status, not be deleted", err)
	}
	if msg.Status != statusProcessed {
		t.Errorf("status = %q, want %q: MoveToFailed must not change the "+
			"status of an already-processed message", msg.Status, statusProcessed)
	}

	var failedCount int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM telegram_game_failed WHERE message_id = $1`, "msg-done").Scan(&failedCount); err != nil {
		t.Fatalf("count failed rows: %v", err)
	}
	if failedCount != 0 {
		t.Errorf("telegram_game_failed has %d rows for an already-processed "+
			"message, want 0: this is what would shrink telegram_game_status "+
			"below the row count history is expected to keep", failedCount)
	}
}

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/queue"
)

type Message struct {
	ID           string
	Attempts     int
	CreatedAt    time.Time
	LastError    string
	LastAttempt  *time.Time
	CurrentState string
}

type Store struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func New(ctx context.Context, dsn string, log zerolog.Logger) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	return &Store{pool: pool, log: log}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	return nil
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS discord_message_status (
			message_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_attempt_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS discord_message_status_status_idx ON discord_message_status (status, created_at)`,
		`CREATE TABLE IF NOT EXISTS discord_message_failed (
			id BIGSERIAL PRIMARY KEY,
			message_id TEXT NOT NULL,
			attempts INTEGER NOT NULL,
			last_error TEXT,
			first_seen_at TIMESTAMPTZ,
			last_attempt_at TIMESTAMPTZ,
			failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			details JSONB
		)`,
		`CREATE INDEX IF NOT EXISTS discord_message_failed_message_id_idx ON discord_message_failed (message_id)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureMessage(ctx context.Context, messageID string) (Message, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO discord_message_status (message_id, status) VALUES ($1, $2) ON CONFLICT DO NOTHING`, messageID, queue.StatusNew); err != nil {
		return Message{}, fmt.Errorf("insert message: %w", err)
	}
	return s.getMessage(ctx, messageID)
}

func (s *Store) getMessage(ctx context.Context, messageID string) (Message, error) {
	var msg Message
	row := s.pool.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at FROM discord_message_status WHERE message_id = $1`, messageID)
	if err := row.Scan(&msg.ID, &msg.CurrentState, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt); err != nil {
		return Message{}, fmt.Errorf("get message: %w", err)
	}
	return msg, nil
}

// ListPending returns messages that still need work: fresh ones, and ones
// abandoned mid-flight by a claim that has gone stale (a crashed or
// hung worker). retryAfter <= 0 disables the staleness check entirely and
// returns every "new" message, which is only safe for the single-threaded
// startup pass that runs before the retry loop and consumer start.
func (s *Store) ListPending(ctx context.Context, limit int, retryAfter time.Duration) ([]Message, error) {
	return s.listPendingAt(ctx, limit, retryAfter, time.Now())
}

// listPendingAt is ListPending with the "now" reference made explicit so
// tests can pin a row's last_attempt_at to precisely
// queue.StaleCutoff(now, retryAfter) and check which side of the SQL
// comparison it falls on -- something not reachable by pinning against a
// live time.Now() call, which always drifts a little between the row
// being written and the query running. Production always goes through
// ListPending.
func (s *Store) listPendingAt(ctx context.Context, limit int, retryAfter time.Duration, now time.Time) ([]Message, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryAfter > 0 {
		cutoff := queue.StaleCutoff(now, retryAfter)
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at
			 FROM discord_message_status
			 WHERE (status = $1 AND (last_attempt_at IS NULL OR last_attempt_at < $3))
			    OR (status = $2 AND last_attempt_at < $3)
			 ORDER BY created_at ASC LIMIT $4`,
			queue.StatusNew, queue.StatusInProgress, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at
			 FROM discord_message_status WHERE status = $1 ORDER BY created_at ASC LIMIT $2`,
			queue.StatusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.CurrentState, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// TouchAttempt claims a message for processing: new -> in_progress, or
// re-claims an in_progress message whose last attempt has gone stale. It
// fails (zero rows affected) when another worker already holds the claim.
// Skipping on failure -- rather than processing anyway -- is what stops
// discord_kafka_processor from handling the same message twice when the
// synchronous consumer and the retry-loop goroutine race on it.
func (s *Store) TouchAttempt(ctx context.Context, messageID string, retryInterval time.Duration) error {
	return s.touchAttemptAt(ctx, messageID, retryInterval, time.Now())
}

// touchAttemptAt is TouchAttempt with the "now" reference made explicit;
// see listPendingAt for why tests need this seam to hit the exact
// staleness boundary. Production always goes through TouchAttempt.
func (s *Store) touchAttemptAt(ctx context.Context, messageID string, retryInterval time.Duration, now time.Time) error {
	cutoff := queue.StaleCutoff(now, retryInterval)
	res, err := s.pool.Exec(
		ctx,
		`UPDATE discord_message_status
		 SET status = $2, last_attempt_at = NOW(), updated_at = NOW()
		 WHERE message_id = $1
		   AND (
			status = $3
			OR (status = $2 AND last_attempt_at < $4)
		   )`,
		messageID,
		queue.StatusInProgress,
		queue.StatusNew,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("touch attempt: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("touch attempt: message not eligible")
	}
	return nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE discord_message_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, queue.StatusProcessed, messageID); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (Message, error) {
	var msg Message
	row := s.pool.QueryRow(ctx, `UPDATE discord_message_status SET attempts = attempts + 1, last_error = $1, last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $2 RETURNING message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at`, errMsg, messageID)
	if err := row.Scan(&msg.ID, &msg.CurrentState, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt); err != nil {
		return Message{}, fmt.Errorf("record attempt: %w", err)
	}
	return msg, nil
}

// MoveToFailed archives a message that has exhausted its retry budget.
//
// It is reached from two independent call sites -- the synchronous
// consumer and the retry-loop goroutine -- that can race on the same
// message_id when a claim is reclaimed after looking stale (see
// TouchAttempt). If the other side already finished successfully, the
// status loaded below is already queue.StatusProcessed by the time this
// runs; deleting the row here would silently drop a completed message
// from discord_message_status, which is exactly the row count the
// migration verifies. So the status fetched for the failed-row insert
// doubles as the guard: a message already processed is left alone instead
// of being archived as failed.
func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details: %w", err)
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var msg Message
		row := tx.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at FROM discord_message_status WHERE message_id = $1`, messageID)
		if err := row.Scan(&msg.ID, &msg.CurrentState, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt); err != nil {
			return fmt.Errorf("load message for fail: %w", err)
		}

		if msg.CurrentState == queue.StatusProcessed {
			s.log.Info().Str("message_id", messageID).
				Msg("skip moving already-processed message to failed table")
			return nil
		}

		if _, err := tx.Exec(ctx, `INSERT INTO discord_message_failed (message_id, attempts, last_error, first_seen_at, last_attempt_at, details) VALUES ($1, $2, $3, $4, $5, $6)`, msg.ID, msg.Attempts, msg.LastError, msg.CreatedAt, msg.LastAttempt, detailsJSON); err != nil {
			return fmt.Errorf("insert failed message: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM discord_message_status WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("delete failed message: %w", err)
		}
		return nil
	})
}

func StatusProcessed() string {
	return queue.StatusProcessed
}

func StatusNew() string {
	return queue.StatusNew
}

func StatusInProgress() string {
	return queue.StatusInProgress
}

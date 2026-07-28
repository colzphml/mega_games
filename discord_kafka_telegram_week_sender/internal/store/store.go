package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/queue"
)

const (
	statusNew       = "new"
	statusProcessed = "processed"
)

type WeekMessage struct {
	ID          string
	Attempts    int
	CreatedAt   time.Time
	LastError   string
	LastAttempt *time.Time
	Payload     string
	Status      string
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
		`CREATE TABLE IF NOT EXISTS telegram_week_status (
			message_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			payload TEXT NOT NULL,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_attempt_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS telegram_week_status_status_idx ON telegram_week_status (status, created_at)`,
		`CREATE TABLE IF NOT EXISTS telegram_week_failed (
			id BIGSERIAL PRIMARY KEY,
			message_id TEXT NOT NULL,
			attempts INTEGER NOT NULL,
			payload TEXT,
			last_error TEXT,
			first_seen_at TIMESTAMPTZ,
			last_attempt_at TIMESTAMPTZ,
			failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			details JSONB
		)`,
		`CREATE INDEX IF NOT EXISTS telegram_week_failed_message_id_idx ON telegram_week_failed (message_id)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureMessage(ctx context.Context, messageID string, payload string) (WeekMessage, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO telegram_week_status (message_id, status, payload) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, messageID, statusNew, payload); err != nil {
		return WeekMessage{}, fmt.Errorf("insert week message: %w", err)
	}
	return s.getMessage(ctx, messageID)
}

func (s *Store) getMessage(ctx context.Context, messageID string) (WeekMessage, error) {
	var msg WeekMessage
	row := s.pool.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_week_status WHERE message_id = $1`, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
		return WeekMessage{}, fmt.Errorf("get week message: %w", err)
	}
	return msg, nil
}

func (s *Store) ListPending(ctx context.Context, limit int, retryInterval time.Duration) ([]WeekMessage, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryInterval > 0 {
		cutoff := queue.StaleCutoff(time.Now(), retryInterval)
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload
			 FROM telegram_week_status
			 WHERE (status = $1 AND (last_attempt_at IS NULL OR last_attempt_at < $3))
			    OR (status = $2 AND last_attempt_at < $3)
			 ORDER BY created_at ASC LIMIT $4`,
			queue.StatusNew, queue.StatusInProgress, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload
			 FROM telegram_week_status
			 WHERE status = $1 ORDER BY created_at ASC LIMIT $2`,
			queue.StatusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []WeekMessage
	for rows.Next() {
		var msg WeekMessage
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// TouchAttempt claims a message for processing. It fails when another
// worker is already sending it, which is what keeps a post from going
// to Telegram twice.
func (s *Store) TouchAttempt(ctx context.Context, messageID string, retryInterval time.Duration) error {
	cutoff := queue.StaleCutoff(time.Now(), retryInterval)
	res, err := s.pool.Exec(
		ctx,
		`UPDATE telegram_week_status
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

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (WeekMessage, error) {
	var msg WeekMessage
	row := s.pool.QueryRow(ctx, `UPDATE telegram_week_status SET attempts = attempts + 1, last_error = $1, last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $2 RETURNING message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload`, errMsg, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
		return WeekMessage{}, fmt.Errorf("record attempt: %w", err)
	}
	return msg, nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE telegram_week_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, statusProcessed, messageID); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var msg WeekMessage
		row := tx.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_week_status WHERE message_id = $1`, messageID)
		if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
			return fmt.Errorf("load message for fail: %w", err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO telegram_week_failed (message_id, attempts, payload, last_error, first_seen_at, last_attempt_at, details) VALUES ($1, $2, $3, $4, $5, $6, $7)`, msg.ID, msg.Attempts, msg.Payload, msg.LastError, msg.CreatedAt, msg.LastAttempt, details); err != nil {
			return fmt.Errorf("insert failed message: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM telegram_week_status WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("delete failed message: %w", err)
		}
		return nil
	})
}

func StatusProcessed() string {
	return statusProcessed
}

func StatusInProgress() string {
	return queue.StatusInProgress
}

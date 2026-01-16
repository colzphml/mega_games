package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	statusNew       = "new"
	statusProcessed = "processed"
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
	if _, err := s.pool.Exec(ctx, `INSERT INTO discord_message_status (message_id, status) VALUES ($1, $2) ON CONFLICT DO NOTHING`, messageID, statusNew); err != nil {
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

func (s *Store) ListPending(ctx context.Context, limit int, retryAfter time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryAfter > 0 {
		cutoff := time.Now().Add(-retryAfter)
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at FROM discord_message_status WHERE status = $1 AND (last_attempt_at IS NULL OR last_attempt_at <= $2) ORDER BY created_at ASC LIMIT $3`, statusNew, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at FROM discord_message_status WHERE status = $1 ORDER BY created_at ASC LIMIT $2`, statusNew, limit)
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

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE discord_message_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, statusProcessed, messageID); err != nil {
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
	return statusProcessed
}

func StatusNew() string {
	return statusNew
}

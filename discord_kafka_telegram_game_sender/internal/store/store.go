package store

import (
	"context"
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

type GameMessage struct {
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
		`CREATE TABLE IF NOT EXISTS telegram_game_status (
			message_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			payload JSONB NOT NULL,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_attempt_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS telegram_game_status_status_idx ON telegram_game_status (status, created_at)`,
		`CREATE TABLE IF NOT EXISTS telegram_game_failed (
			id BIGSERIAL PRIMARY KEY,
			message_id TEXT NOT NULL,
			attempts INTEGER NOT NULL,
			payload JSONB,
			last_error TEXT,
			first_seen_at TIMESTAMPTZ,
			last_attempt_at TIMESTAMPTZ,
			failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			details JSONB
		)`,
		`CREATE INDEX IF NOT EXISTS telegram_game_failed_message_id_idx ON telegram_game_failed (message_id)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureMessage(ctx context.Context, messageID string, payload string) (GameMessage, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO telegram_game_status (message_id, status, payload) VALUES ($1, $2, $3::jsonb) ON CONFLICT DO NOTHING`, messageID, statusNew, payload); err != nil {
		return GameMessage{}, fmt.Errorf("insert game message: %w", err)
	}
	return s.getMessage(ctx, messageID)
}

func (s *Store) getMessage(ctx context.Context, messageID string) (GameMessage, error) {
	var msg GameMessage
	row := s.pool.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_game_status WHERE message_id = $1`, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
		return GameMessage{}, fmt.Errorf("get game message: %w", err)
	}
	return msg, nil
}

func (s *Store) ListPending(ctx context.Context, limit int, retryAfter time.Duration) ([]GameMessage, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryAfter > 0 {
		cutoff := time.Now().Add(-retryAfter)
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_game_status WHERE status = $1 AND (last_attempt_at IS NULL OR last_attempt_at <= $2) ORDER BY created_at ASC LIMIT $3`, statusNew, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_game_status WHERE status = $1 ORDER BY created_at ASC LIMIT $2`, statusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []GameMessage
	for rows.Next() {
		var msg GameMessage
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *Store) TouchAttempt(ctx context.Context, messageID string) error {
	res, err := s.pool.Exec(ctx, `UPDATE telegram_game_status SET last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $1`, messageID)
	if err != nil {
		return fmt.Errorf("touch attempt: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("touch attempt: message not found")
	}
	return nil
}

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (GameMessage, error) {
	var msg GameMessage
	row := s.pool.QueryRow(ctx, `UPDATE telegram_game_status SET attempts = attempts + 1, last_error = $1, last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $2 RETURNING message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload`, errMsg, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
		return GameMessage{}, fmt.Errorf("record attempt: %w", err)
	}
	return msg, nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE telegram_game_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, statusProcessed, messageID); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var msg GameMessage
		row := tx.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM telegram_game_status WHERE message_id = $1`, messageID)
		if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
			return fmt.Errorf("load message for fail: %w", err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO telegram_game_failed (message_id, attempts, payload, last_error, first_seen_at, last_attempt_at, details) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7)`, msg.ID, msg.Attempts, msg.Payload, msg.LastError, msg.CreatedAt, msg.LastAttempt, details); err != nil {
			return fmt.Errorf("insert failed message: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM telegram_game_status WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("delete failed message: %w", err)
		}
		return nil
	})
}

func StatusProcessed() string {
	return statusProcessed
}

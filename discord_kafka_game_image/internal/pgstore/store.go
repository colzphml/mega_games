package pgstore

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

const (
	statusNew        = "new"
	statusInProgress = "in_progress"
	statusProcessed  = "processed"
)

type Message struct {
	ID          string
	Attempts    int
	CreatedAt   time.Time
	LastError   string
	LastAttempt *time.Time
	Payload     []byte
	Image       []byte
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
		`CREATE TABLE IF NOT EXISTS game_image_status (
			message_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			payload JSONB NOT NULL,
			image JSONB,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_attempt_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS game_image_status_status_idx ON game_image_status (status, created_at)`,
		`CREATE TABLE IF NOT EXISTS game_image_failed (
			id BIGSERIAL PRIMARY KEY,
			message_id TEXT NOT NULL,
			attempts INTEGER NOT NULL,
			payload JSONB,
			image JSONB,
			last_error TEXT,
			first_seen_at TIMESTAMPTZ,
			last_attempt_at TIMESTAMPTZ,
			failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			details JSONB
		)`,
		`CREATE INDEX IF NOT EXISTS game_image_failed_message_id_idx ON game_image_failed (message_id)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureMessage(ctx context.Context, messageID string, payload []byte) (Message, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO game_image_status (message_id, status, payload) VALUES ($1, $2, $3::jsonb) ON CONFLICT DO NOTHING`, messageID, statusNew, payload); err != nil {
		return Message{}, fmt.Errorf("insert game image message: %w", err)
	}
	return s.getMessage(ctx, messageID)
}

func (s *Store) getMessage(ctx context.Context, messageID string) (Message, error) {
	var msg Message
	row := s.pool.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image FROM game_image_status WHERE message_id = $1`, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload, &msg.Image); err != nil {
		return Message{}, fmt.Errorf("get game image message: %w", err)
	}
	return msg, nil
}

func (s *Store) TouchAttempt(ctx context.Context, messageID string, retryAfter time.Duration) error {
	cutoff := queue.StaleCutoff(time.Now(), retryAfter)
	res, err := s.pool.Exec(
		ctx,
		`UPDATE game_image_status
		 SET status = $2, last_attempt_at = NOW(), updated_at = NOW()
		 WHERE message_id = $1
		   AND (
			status = $3
			OR (status = $2 AND (last_attempt_at IS NULL OR last_attempt_at < $4))
		   )`,
		messageID,
		statusInProgress,
		statusNew,
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

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (Message, error) {
	var msg Message
	row := s.pool.QueryRow(ctx, `UPDATE game_image_status SET attempts = attempts + 1, last_error = $1, last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $2 RETURNING message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image`, errMsg, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload, &msg.Image); err != nil {
		return Message{}, fmt.Errorf("record attempt: %w", err)
	}
	return msg, nil
}

func (s *Store) UpdateImage(ctx context.Context, messageID string, imageJSON []byte) error {
	if _, err := s.pool.Exec(ctx, `UPDATE game_image_status SET image = $1::jsonb, updated_at = NOW() WHERE message_id = $2`, imageJSON, messageID); err != nil {
		return fmt.Errorf("update image: %w", err)
	}
	return nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE game_image_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, statusProcessed, messageID); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

// MoveToFailed archives a message that has exhausted its retry budget.
//
// reprocessPending and consumeLoop can both reach here for the same
// message_id after a stale in_progress claim gets reclaimed (see
// TouchAttempt): the reclaiming attempt may have already finished and
// called MarkProcessed by the time the original, belated attempt's own
// failure exhausts its budget and lands here. If so, the status loaded
// below is already statusProcessed; deleting the row in that case would
// silently drop a successfully-delivered message from game_image_status.
// So the status fetched for the failed-row insert doubles as the guard:
// an already-processed message is left alone instead of being archived
// as failed.
func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details: %w", err)
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var msg Message
		row := tx.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image FROM game_image_status WHERE message_id = $1`, messageID)
		if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload, &msg.Image); err != nil {
			return fmt.Errorf("load message for fail: %w", err)
		}

		if msg.Status == statusProcessed {
			s.log.Info().Str("message_id", messageID).
				Msg("skip moving already-processed message to failed table")
			return nil
		}

		if _, err := tx.Exec(ctx, `INSERT INTO game_image_failed (message_id, attempts, payload, image, last_error, first_seen_at, last_attempt_at, details) VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6, $7, $8::jsonb)`, msg.ID, msg.Attempts, msg.Payload, msg.Image, msg.LastError, msg.CreatedAt, msg.LastAttempt, detailsJSON); err != nil {
			return fmt.Errorf("insert failed message: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM game_image_status WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("delete failed message: %w", err)
		}
		return nil
	})
}

// ListPending returns messages that still need work: fresh ones, and
// ones abandoned mid-flight by a crashed worker. It replaces the Mongo
// implementation that reprocessPending relied on.
func (s *Store) ListPending(ctx context.Context, limit int, retryInterval time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 1000
	}

	var rows pgx.Rows
	var err error
	if retryInterval > 0 {
		cutoff := queue.StaleCutoff(time.Now(), retryInterval)
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image
			 FROM game_image_status
			 WHERE (status = $1 AND (last_attempt_at IS NULL OR last_attempt_at < $3))
			    OR (status = $2 AND last_attempt_at < $3)
			 ORDER BY created_at ASC LIMIT $4`,
			queue.StatusNew, queue.StatusInProgress, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image
			 FROM game_image_status
			 WHERE status = $1 ORDER BY created_at ASC LIMIT $2`,
			queue.StatusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt,
			&msg.LastError, &msg.LastAttempt, &msg.Payload, &msg.Image); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func StatusProcessed() string {
	return statusProcessed
}

func StatusInProgress() string {
	return statusInProgress
}

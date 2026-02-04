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

type WeekPayload struct {
	Season string `json:"season"`
	Week   int    `json:"week"`
	Title  string `json:"title,omitempty"`
}

type WeekMessage struct {
	ID          string
	Attempts    int
	CreatedAt   time.Time
	LastError   string
	LastAttempt *time.Time
	Payload     WeekPayload
	Status      string
}

type Team struct {
	Name      string
	ShortName string
	Player    string
}

type Game struct {
	Home string
	Away string
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
		`CREATE TABLE IF NOT EXISTS week_message_status (
			message_id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			payload JSONB NOT NULL,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_attempt_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS week_message_status_status_idx ON week_message_status (status, created_at)`,
		`CREATE TABLE IF NOT EXISTS week_message_failed (
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
		`CREATE INDEX IF NOT EXISTS week_message_failed_message_id_idx ON week_message_failed (message_id)`,
		`CREATE TABLE IF NOT EXISTS teams (
			name TEXT PRIMARY KEY,
			short_name TEXT NOT NULL,
			emoji TEXT,
			player TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_games (
			id BIGSERIAL PRIMARY KEY,
			season_index INT NOT NULL,
			stage BOOLEAN NOT NULL,
			week_index INT NOT NULL,
			home_team TEXT NOT NULL,
			away_team TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS schedule_games_lookup_idx ON schedule_games (season_index, stage, week_index)`,
		`DELETE FROM schedule_games a USING schedule_games b
			WHERE a.id < b.id
			  AND a.season_index = b.season_index
			  AND a.stage = b.stage
			  AND a.week_index = b.week_index
			  AND a.home_team = b.home_team
			  AND a.away_team = b.away_team`,
		`CREATE UNIQUE INDEX IF NOT EXISTS schedule_games_unique_idx
			ON schedule_games (season_index, stage, week_index, home_team, away_team)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureWeekMessage(ctx context.Context, messageID string, payload WeekPayload) (WeekMessage, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return WeekMessage{}, fmt.Errorf("marshal payload: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO week_message_status (message_id, status, payload) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, messageID, statusNew, payloadJSON); err != nil {
		return WeekMessage{}, fmt.Errorf("insert week message: %w", err)
	}
	return s.getWeekMessage(ctx, messageID)
}

func (s *Store) getWeekMessage(ctx context.Context, messageID string) (WeekMessage, error) {
	var msg WeekMessage
	var payloadJSON []byte
	row := s.pool.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM week_message_status WHERE message_id = $1`, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &payloadJSON); err != nil {
		return WeekMessage{}, fmt.Errorf("get week message: %w", err)
	}
	if err := json.Unmarshal(payloadJSON, &msg.Payload); err != nil {
		return WeekMessage{}, fmt.Errorf("decode payload: %w", err)
	}
	return msg, nil
}

func (s *Store) ListPending(ctx context.Context, limit int, retryAfter time.Duration) ([]WeekMessage, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryAfter > 0 {
		cutoff := time.Now().Add(-retryAfter)
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM week_message_status WHERE status = $1 AND (last_attempt_at IS NULL OR last_attempt_at <= $2) ORDER BY created_at ASC LIMIT $3`, statusNew, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM week_message_status WHERE status = $1 ORDER BY created_at ASC LIMIT $2`, statusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []WeekMessage
	for rows.Next() {
		var msg WeekMessage
		var payloadJSON []byte
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		if err := json.Unmarshal(payloadJSON, &msg.Payload); err != nil {
			return nil, fmt.Errorf("decode payload: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (WeekMessage, error) {
	var msg WeekMessage
	var payloadJSON []byte
	row := s.pool.QueryRow(ctx, `UPDATE week_message_status SET attempts = attempts + 1, last_error = $1, last_attempt_at = NOW(), updated_at = NOW() WHERE message_id = $2 RETURNING message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload`, errMsg, messageID)
	if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &payloadJSON); err != nil {
		return WeekMessage{}, fmt.Errorf("record attempt: %w", err)
	}
	if err := json.Unmarshal(payloadJSON, &msg.Payload); err != nil {
		return WeekMessage{}, fmt.Errorf("decode payload: %w", err)
	}
	return msg, nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE week_message_status SET status = $1, updated_at = NOW() WHERE message_id = $2`, statusProcessed, messageID); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details: %w", err)
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var msg WeekMessage
		var payloadJSON []byte
		row := tx.QueryRow(ctx, `SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload FROM week_message_status WHERE message_id = $1`, messageID)
		if err := row.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &payloadJSON); err != nil {
			return fmt.Errorf("load message for fail: %w", err)
		}
		if err := json.Unmarshal(payloadJSON, &msg.Payload); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO week_message_failed (message_id, attempts, payload, last_error, first_seen_at, last_attempt_at, details) VALUES ($1, $2, $3, $4, $5, $6, $7)`, msg.ID, msg.Attempts, payloadJSON, msg.LastError, msg.CreatedAt, msg.LastAttempt, detailsJSON); err != nil {
			return fmt.Errorf("insert failed message: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM week_message_status WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("delete failed message: %w", err)
		}
		return nil
	})
}

func (s *Store) LoadTeams(ctx context.Context) (map[string]Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, short_name, player FROM teams`)
	if err != nil {
		return nil, fmt.Errorf("load teams: %w", err)
	}
	defer rows.Close()

	teams := make(map[string]Team)
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.Name, &team.ShortName, &team.Player); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams[team.Name] = team
	}
	return teams, rows.Err()
}

func (s *Store) LoadGamesForWeek(ctx context.Context, season string, week int) ([]Game, error) {
	if week < 1 {
		return nil, nil
	}
	if season == "preseason" && week == 4 {
		return nil, nil
	}

	var seasonIndex int
	row := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(season_index), -1) FROM schedule_games`)
	if err := row.Scan(&seasonIndex); err != nil {
		return nil, fmt.Errorf("load max season: %w", err)
	}
	if seasonIndex < 0 {
		return nil, nil
	}

	stage := season == "regular"
	weekIndex := week - 1

	rows, err := s.pool.Query(ctx, `SELECT home_team, away_team FROM schedule_games WHERE season_index = $1 AND stage = $2 AND week_index = $3`, seasonIndex, stage, weekIndex)
	if err != nil {
		return nil, fmt.Errorf("load games: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var game Game
		if err := rows.Scan(&game.Home, &game.Away); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, game)
	}
	return games, rows.Err()
}

func StatusProcessed() string {
	return statusProcessed
}

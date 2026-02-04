package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Team struct {
	Name      string
	ShortName string
	Emoji     string
	Player    string
}

type Game struct {
	SeasonIndex int
	Stage       bool
	WeekIndex   int
	Home        string
	Away        string
}

type UnifiedMessage struct {
	MessageID           string
	CreatedAt           time.Time
	ProcessorStatus     string
	WeekFormatterStatus string
	GameImageStatus     string
	TelegramWeekStatus  string
	TelegramGameStatus  string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.pool.Query(ctx, "SELECT name, short_name, emoji, player FROM teams ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("failed to query teams: %w", err)
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		var emoji *string
		if err := rows.Scan(&t.Name, &t.ShortName, &emoji, &t.Player); err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		if emoji != nil {
			t.Emoji = *emoji
		}
		teams = append(teams, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return teams, nil
}

func (s *Store) GetTeam(ctx context.Context, name string) (Team, error) {
	var t Team
	var emoji *string
	err := s.pool.QueryRow(ctx, "SELECT name, short_name, emoji, player FROM teams WHERE name = $1", name).
		Scan(&t.Name, &t.ShortName, &emoji, &t.Player)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Team{}, fmt.Errorf("%w: team %s", ErrNotFound, name)
		}
		return Team{}, fmt.Errorf("failed to get team: %w", err)
	}
	if emoji != nil {
		t.Emoji = *emoji
	}
	return t, nil
}

func (s *Store) CreateTeam(ctx context.Context, t Team) error {
	tag, err := s.pool.Exec(ctx,
		"INSERT INTO teams (name, short_name, emoji, player) VALUES ($1, $2, $3, $4) ON CONFLICT (name) DO NOTHING",
		t.Name, t.ShortName, t.Emoji, t.Player,
	)
	if err != nil {
		return fmt.Errorf("failed to create team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: team %s", ErrConflict, t.Name)
	}
	return nil
}

func (s *Store) UpdateTeam(ctx context.Context, t Team) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE teams SET short_name = $1, emoji = $2, player = $3 WHERE name = $4",
		t.ShortName, t.Emoji, t.Player, t.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: team %s", ErrNotFound, t.Name)
	}
	return nil
}

func (s *Store) DeleteTeam(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM teams WHERE name = $1", name)
	if err != nil {
		return fmt.Errorf("failed to delete team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: team %s", ErrNotFound, name)
	}
	return nil
}

func (s *Store) UpsertGame(ctx context.Context, g Game) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO schedule_games (season_index, stage, week_index, home_team, away_team)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (season_index, stage, week_index, home_team, away_team)
		DO NOTHING
	`, g.SeasonIndex, g.Stage, g.WeekIndex, g.Home, g.Away)
	if err != nil {
		return fmt.Errorf("failed to upsert game: %w", err)
	}
	return nil
}

func (s *Store) GetStatusCounts(ctx context.Context) (map[string]map[string]int, error) {
	tables := []string{
		"discord_message_status",
		"week_message_status",
		"telegram_week_status",
		"telegram_game_status",
		"game_image_status",
	}

	result := make(map[string]map[string]int)

	for _, table := range tables {
		counts := make(map[string]int)
		query := fmt.Sprintf("SELECT status, COUNT(*) FROM %s GROUP BY status", table)
		rows, err := s.pool.Query(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to query status for %s: %w", table, err)
		}

		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan status for %s: %w", table, err)
			}
			counts[status] = count
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("rows iteration error for %s: %w", table, err)
		}
		rows.Close()
		result[table] = counts
	}

	return result, nil
}

func (s *Store) ListUnifiedMessages(ctx context.Context, limit int) ([]UnifiedMessage, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		WITH all_messages AS (
			SELECT message_id, created_at FROM discord_message_status
			UNION ALL
			SELECT message_id, created_at FROM week_message_status
			UNION ALL
			SELECT message_id, created_at FROM game_image_status
			UNION ALL
			SELECT message_id, created_at FROM telegram_week_status
			UNION ALL
			SELECT message_id, created_at FROM telegram_game_status
		),
		base AS (
			SELECT message_id, MAX(created_at) AS created_at
			FROM all_messages
			GROUP BY message_id
		)
		SELECT
			base.message_id,
			base.created_at,
			d.status AS processor_status,
			w.status AS week_formatter_status,
			g.status AS game_image_status,
			tw.status AS telegram_week_status,
			tg.status AS telegram_game_status
		FROM base
		LEFT JOIN discord_message_status d ON d.message_id = base.message_id
		LEFT JOIN week_message_status w ON w.message_id = base.message_id
		LEFT JOIN game_image_status g ON g.message_id = base.message_id
		LEFT JOIN telegram_week_status tw ON tw.message_id = base.message_id
		LEFT JOIN telegram_game_status tg ON tg.message_id = base.message_id
		ORDER BY base.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unified messages: %w", err)
	}
	defer rows.Close()

	var messages []UnifiedMessage
	for rows.Next() {
		var msg UnifiedMessage
		var processorStatus *string
		var weekFormatterStatus *string
		var gameImageStatus *string
		var telegramWeekStatus *string
		var telegramGameStatus *string
		if err := rows.Scan(
			&msg.MessageID,
			&msg.CreatedAt,
			&processorStatus,
			&weekFormatterStatus,
			&gameImageStatus,
			&telegramWeekStatus,
			&telegramGameStatus,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unified message: %w", err)
		}
		msg.ProcessorStatus = normalizeStatus(processorStatus)
		msg.WeekFormatterStatus = normalizeStatus(weekFormatterStatus)
		msg.GameImageStatus = normalizeStatus(gameImageStatus)
		msg.TelegramWeekStatus = normalizeStatus(telegramWeekStatus)
		msg.TelegramGameStatus = normalizeStatus(telegramGameStatus)
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return messages, nil
}

func normalizeStatus(status *string) string {
	if status == nil || *status == "" {
		return "n/a"
	}
	return *status
}

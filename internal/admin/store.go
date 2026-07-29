package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	GameID              string
	ImageURL            string
	Fetcher             string
	WeekText            string
	Errors              []string
	DropReason          string
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

// unifiedSelectTail is the projection, six-way LEFT JOIN, and final
// ordering shared by ListUnifiedMessages. It intentionally carries no
// placeholders of its own: $1 (limit) and $2 (cutoff) are both consumed
// by the CTE that precedes it, so these JOINs run against the at-most-
// `limit` rows `base` already narrowed down to, not the full tables.
const unifiedSelectTail = `
		base.message_id,
		base.created_at,
		d.status AS processor_status,
		w.status AS week_formatter_status,
		CASE
			WHEN g.status IS NOT NULL THEN g.status
			WHEN gf.message_id IS NOT NULL THEN 'failed'
			ELSE NULL
		END AS game_image_status,
		tw.status AS telegram_week_status,
		tg.status AS telegram_game_status,
		COALESCE(g.payload->>'game_id', gf.payload->>'game_id') AS game_id,
		COALESCE(g.image->>'image_url', gf.image->>'image_url') AS image_url,
		COALESCE(g.image->>'fetcher', gf.image->>'fetcher') AS image_fetcher,
		w.payload->>'title' AS week_title,
		w.payload->>'season' AS week_season,
		w.payload->>'week' AS week_number,
		d.last_error AS processor_error,
		w.last_error AS week_formatter_error,
		COALESCE(g.last_error, gf.last_error) AS game_image_error,
		tw.last_error AS telegram_week_error,
		tg.last_error AS telegram_game_error,
		d.updated_at AS processor_updated_at,
		w.updated_at AS week_formatter_updated_at,
		COALESCE(g.updated_at, gf.last_attempt_at, gf.failed_at) AS game_image_updated_at,
		tw.updated_at AS telegram_week_updated_at,
		tg.updated_at AS telegram_game_updated_at,
		gf.details->>'reason' AS game_image_failed_reason,
		gf.details->>'source' AS game_image_failed_source
	FROM base
	LEFT JOIN discord_message_status d ON d.message_id = base.message_id
	LEFT JOIN week_message_status w ON w.message_id = base.message_id
	LEFT JOIN game_image_status g ON g.message_id = base.message_id
	LEFT JOIN game_image_failed gf ON gf.message_id = base.message_id
	LEFT JOIN telegram_week_status tw ON tw.message_id = base.message_id
	LEFT JOIN telegram_game_status tg ON tg.message_id = base.message_id
	ORDER BY base.created_at DESC
`

// ListUnifiedMessages returns recent pipeline activity.
//
// The window matters: the CTE unions six status tables and groups the
// result, so without a date bound every dashboard render scanned every
// row ever written — that scan is where the idle admin panel's CPU went.
// History is untouched; only the view is bounded. LIMIT and the first
// ORDER BY live inside the `base` CTE, so the six LEFT JOINs in
// unifiedSelectTail run against at most `limit` rows instead of the full
// status tables. The trailing ORDER BY in unifiedSelectTail is not
// redundant: a LEFT JOIN is free to reorder rows, so without it the
// result order would no longer be guaranteed to match `base`.
func (s *Store) ListUnifiedMessages(ctx context.Context, limit int, window time.Duration) ([]UnifiedMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-window)

	rows, err := s.pool.Query(ctx, `
		WITH all_messages AS (
			SELECT message_id, created_at FROM discord_message_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM week_message_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM game_image_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, COALESCE(last_attempt_at, failed_at, first_seen_at) AS created_at
			  FROM game_image_failed
			 WHERE COALESCE(last_attempt_at, failed_at, first_seen_at) > $2
			UNION ALL
			SELECT message_id, created_at FROM telegram_week_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM telegram_game_status WHERE created_at > $2
		),
		base AS (
			SELECT message_id, MAX(created_at) AS created_at
			FROM all_messages
			GROUP BY message_id
			ORDER BY created_at DESC
			LIMIT $1
		)
		SELECT
	`+unifiedSelectTail, limit, cutoff)
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
		var gameID *string
		var imageURL *string
		var imageFetcher *string
		var weekTitle *string
		var weekSeason *string
		var weekNumber *string
		var processorError *string
		var weekFormatterError *string
		var gameImageError *string
		var telegramWeekError *string
		var telegramGameError *string
		var processorUpdatedAt *time.Time
		var weekFormatterUpdatedAt *time.Time
		var gameImageUpdatedAt *time.Time
		var telegramWeekUpdatedAt *time.Time
		var telegramGameUpdatedAt *time.Time
		var gameImageFailedReason *string
		var gameImageFailedSource *string
		if err := rows.Scan(
			&msg.MessageID,
			&msg.CreatedAt,
			&processorStatus,
			&weekFormatterStatus,
			&gameImageStatus,
			&telegramWeekStatus,
			&telegramGameStatus,
			&gameID,
			&imageURL,
			&imageFetcher,
			&weekTitle,
			&weekSeason,
			&weekNumber,
			&processorError,
			&weekFormatterError,
			&gameImageError,
			&telegramWeekError,
			&telegramGameError,
			&processorUpdatedAt,
			&weekFormatterUpdatedAt,
			&gameImageUpdatedAt,
			&telegramWeekUpdatedAt,
			&telegramGameUpdatedAt,
			&gameImageFailedReason,
			&gameImageFailedSource,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unified message: %w", err)
		}
		msg.ProcessorStatus = normalizeStatus(processorStatus)
		msg.WeekFormatterStatus = normalizeStatus(weekFormatterStatus)
		msg.GameImageStatus = normalizeStatus(gameImageStatus)
		msg.TelegramWeekStatus = normalizeStatus(telegramWeekStatus)
		msg.TelegramGameStatus = normalizeStatus(telegramGameStatus)
		msg.GameID = normalizeValue(gameID)
		msg.ImageURL = normalizeValue(imageURL)
		msg.Fetcher = normalizeValue(imageFetcher)
		msg.WeekText = formatWeekText(weekTitle, weekSeason, weekNumber)
		msg.Errors, msg.DropReason = buildErrorDetails(errorCandidates{
			processor: errorCandidate{message: processorError, updatedAt: processorUpdatedAt},
			week:      errorCandidate{message: weekFormatterError, updatedAt: weekFormatterUpdatedAt},
			image:     errorCandidate{message: gameImageError, updatedAt: gameImageUpdatedAt},
			tgWeek:    errorCandidate{message: telegramWeekError, updatedAt: telegramWeekUpdatedAt},
			tgGame:    errorCandidate{message: telegramGameError, updatedAt: telegramGameUpdatedAt},
		})
		if reason := normalizeValue(gameImageFailedReason); reason != "" {
			dropReason := "game_image: " + reason
			if source := normalizeValue(gameImageFailedSource); source != "" {
				dropReason += " (source: " + source + ")"
			}
			msg.DropReason = dropReason
		}
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

func normalizeValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func formatWeekText(title *string, season *string, week *string) string {
	if titleText := normalizeValue(title); titleText != "" {
		return titleText
	}
	seasonText := normalizeValue(season)
	weekText := normalizeValue(week)
	if seasonText == "" && weekText == "" {
		return ""
	}
	if seasonText != "" && weekText != "" {
		return fmt.Sprintf("%s week %s", seasonText, weekText)
	}
	if seasonText != "" {
		return seasonText
	}
	return fmt.Sprintf("week %s", weekText)
}

type errorCandidate struct {
	message   *string
	updatedAt *time.Time
}

type errorCandidates struct {
	processor errorCandidate
	week      errorCandidate
	image     errorCandidate
	tgWeek    errorCandidate
	tgGame    errorCandidate
}

type errorEntry struct {
	component string
	message   string
	updatedAt *time.Time
}

func buildErrorDetails(candidates errorCandidates) ([]string, string) {
	entries := make([]errorEntry, 0, 5)
	entries = appendErrorEntry(entries, "processor", candidates.processor)
	entries = appendErrorEntry(entries, "week_formatter", candidates.week)
	entries = appendErrorEntry(entries, "game_image", candidates.image)
	entries = appendErrorEntry(entries, "telegram_week", candidates.tgWeek)
	entries = appendErrorEntry(entries, "telegram_game", candidates.tgGame)
	if len(entries) == 0 {
		return nil, ""
	}

	formatted := make([]string, 0, len(entries))
	for _, entry := range entries {
		formatted = append(formatted, fmt.Sprintf("%s: %s", entry.component, entry.message))
	}

	latest := entries[0]
	for _, entry := range entries[1:] {
		if entry.updatedAt == nil {
			continue
		}
		if latest.updatedAt == nil || entry.updatedAt.After(*latest.updatedAt) {
			latest = entry
		}
	}

	return formatted, fmt.Sprintf("%s: %s", latest.component, latest.message)
}

func appendErrorEntry(entries []errorEntry, component string, candidate errorCandidate) []errorEntry {
	if candidate.message == nil {
		return entries
	}
	message := strings.TrimSpace(*candidate.message)
	if message == "" {
		return entries
	}
	return append(entries, errorEntry{component: component, message: message, updatedAt: candidate.updatedAt})
}

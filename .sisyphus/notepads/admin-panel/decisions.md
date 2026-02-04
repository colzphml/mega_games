# Decisions (admin-panel)

## 2026-02-01 Task: schedule-dedup-unique
- Use a Postgres UNIQUE index (not a named constraint) for idempotent schedule ingestion:
  - `CREATE UNIQUE INDEX IF NOT EXISTS schedule_games_unique_idx ON schedule_games (season_index, stage, week_index, home_team, away_team)`
- For existing deployments, run a one-time dedupe before creating the index:
  - Use `ROW_NUMBER() OVER (PARTITION BY season_index, stage, week_index, home_team, away_team ORDER BY id DESC)` and delete rows with `rn > 1`.
- Keep this index creation in BOTH schema sources:
  - `discord_tools/sql/schema.sql` (manual bootstrap)
  - `discord_kafka_week_formatter/internal/store/store.go` EnsureSchema (auto bootstrap)

## 2026-02-01 Task: admin-store-logic
- Implemented `internal/admin/store.go` using `pgxpool`.
- `Team` struct mirrors the database schema `teams` table.
- Handled `emoji` column as nullable:
  - Schema defines `emoji TEXT` (nullable).
  - Struct uses `Emoji string`.
  - Used `*string` intermediate variable during `Scan` to safely handle potential SQL NULLs, defaulting to empty string in the struct.
- Implemented standard CRUD operations: List, Get, Create, Update, Delete.

## 2026-02-01 Task: dashboard-ui
- Created `DashboardHandler` to manage read-only views (Dashboard, Logs, Schedule form).
- Kept `ScheduleHandler` for the actual data processing (Upload POST).
- Used separate template parsing for each dashboard view (`NewDashboardHandler`) to prevent `block "content"` conflicts while reusing `layout.html`.
- Implemented `GetStatusCounts` in Store to aggregate stats from 5 services using `map[string]map[string]int` structure.

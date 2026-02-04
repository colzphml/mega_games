### Decisions
- Used 'MEGA_games.csv' as the multipart form field name as per task description.
- Followed CSV parsing logic from 'discord_tools/cmd/csv-to-sql/main.go'.
- Implemented idempotent upsert using ON CONFLICT on (season_index, stage, week_index, home_team, away_team).

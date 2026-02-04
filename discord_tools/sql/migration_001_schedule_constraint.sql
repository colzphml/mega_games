BEGIN;

-- Deduplicate: keep the row with the highest ID for each unique game combination
DELETE FROM schedule_games a USING schedule_games b
WHERE a.id < b.id
  AND a.season_index = b.season_index
  AND a.stage = b.stage
  AND a.week_index = b.week_index
  AND a.home_team = b.home_team
  AND a.away_team = b.away_team;

-- Create Unique Index
CREATE UNIQUE INDEX IF NOT EXISTS schedule_games_unique_idx
ON schedule_games (season_index, stage, week_index, home_team, away_team);

COMMIT;

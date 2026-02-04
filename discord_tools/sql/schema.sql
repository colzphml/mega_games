CREATE TABLE IF NOT EXISTS teams (
    name TEXT PRIMARY KEY,
    short_name TEXT NOT NULL,
    emoji TEXT,
    player TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schedule_games (
    id BIGSERIAL PRIMARY KEY,
    season_index INT NOT NULL,
    stage BOOLEAN NOT NULL,
    week_index INT NOT NULL,
    home_team TEXT NOT NULL,
    away_team TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS schedule_games_lookup_idx
    ON schedule_games (season_index, stage, week_index);

CREATE UNIQUE INDEX IF NOT EXISTS schedule_games_unique_idx
    ON schedule_games (season_index, stage, week_index, home_team, away_team);

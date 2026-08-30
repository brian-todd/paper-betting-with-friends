-- Add external ID and additional fields to venues.
ALTER TABLE venues
    ADD COLUMN IF NOT EXISTS external_id BIGINT UNIQUE,
    ADD COLUMN IF NOT EXISTS timezone VARCHAR(50),
    ADD COLUMN IF NOT EXISTS dome BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS grass BOOLEAN DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_venues_external_id ON venues(external_id);

-- Add external ID and additional fields to teams.
ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS external_id BIGINT UNIQUE,
    ADD COLUMN IF NOT EXISTS mascot VARCHAR(100),
    ADD COLUMN IF NOT EXISTS logo_url TEXT,
    ADD COLUMN IF NOT EXISTS primary_color VARCHAR(7),
    ADD COLUMN IF NOT EXISTS secondary_color VARCHAR(7),
    ADD COLUMN IF NOT EXISTS classification VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_teams_external_id ON teams(external_id);

-- Add season type to weeks.
ALTER TABLE weeks
    ADD COLUMN IF NOT EXISTS season_type VARCHAR(20) DEFAULT 'regular';

-- Add external ID and additional fields to games.
ALTER TABLE games
    ADD COLUMN IF NOT EXISTS external_id BIGINT UNIQUE,
    ADD COLUMN IF NOT EXISTS neutral_site BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS conference_game BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS completed BOOLEAN DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_games_external_id ON games(external_id);

-- Create game_results table.
CREATE TABLE IF NOT EXISTS game_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL UNIQUE REFERENCES games(id) ON DELETE CASCADE,
    home_score INTEGER NOT NULL,
    away_score INTEGER NOT NULL,
    home_line_scores JSONB,
    away_line_scores JSONB,
    excitement_index DECIMAL(5,2),
    finalized_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_game_results_game_id ON game_results(game_id);

-- Add unique constraints to odds tables for upsert support.
CREATE UNIQUE INDEX IF NOT EXISTS idx_money_line_odds_game_source ON money_line_odds(game_id, source);
CREATE UNIQUE INDEX IF NOT EXISTS idx_spread_odds_game_source ON spread_odds(game_id, source);
CREATE UNIQUE INDEX IF NOT EXISTS idx_over_under_odds_game_source ON over_under_odds(game_id, source);

-- Migrate existing scores from games to game_results.
INSERT INTO game_results (game_id, home_score, away_score, finalized_at)
SELECT id, home_score, away_score, updated_at
FROM games
WHERE home_score IS NOT NULL AND away_score IS NOT NULL
ON CONFLICT (game_id) DO NOTHING;

-- Drop score columns from games.
ALTER TABLE games
    DROP COLUMN IF EXISTS home_score,
    DROP COLUMN IF EXISTS away_score;

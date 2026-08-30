-- Add score columns back to games.
ALTER TABLE games
    ADD COLUMN IF NOT EXISTS home_score INTEGER,
    ADD COLUMN IF NOT EXISTS away_score INTEGER;

-- Migrate scores back from game_results to games.
UPDATE games
SET home_score = gr.home_score, away_score = gr.away_score
FROM game_results gr
WHERE games.id = gr.game_id;

-- Drop game_results table.
DROP TABLE IF EXISTS game_results;

-- Remove unique constraints from odds tables.
DROP INDEX IF EXISTS idx_money_line_odds_game_source;
DROP INDEX IF EXISTS idx_spread_odds_game_source;
DROP INDEX IF EXISTS idx_over_under_odds_game_source;

-- Remove additional fields from games.
DROP INDEX IF EXISTS idx_games_external_id;
ALTER TABLE games
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS neutral_site,
    DROP COLUMN IF EXISTS conference_game,
    DROP COLUMN IF EXISTS completed;

-- Remove season type from weeks.
ALTER TABLE weeks
    DROP COLUMN IF EXISTS season_type;

-- Remove additional fields from teams.
DROP INDEX IF EXISTS idx_teams_external_id;
ALTER TABLE teams
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS mascot,
    DROP COLUMN IF EXISTS logo_url,
    DROP COLUMN IF EXISTS primary_color,
    DROP COLUMN IF EXISTS secondary_color,
    DROP COLUMN IF EXISTS classification;

-- Remove additional fields from venues.
DROP INDEX IF EXISTS idx_venues_external_id;
ALTER TABLE venues
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS dome,
    DROP COLUMN IF EXISTS grass;

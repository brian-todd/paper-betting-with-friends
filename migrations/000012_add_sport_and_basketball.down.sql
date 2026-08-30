-- Drop new indexes.
DROP INDEX IF EXISTS idx_games_tournament;
DROP INDEX IF EXISTS idx_games_sport_season;
DROP INDEX IF EXISTS idx_games_sport_scheduled;
DROP INDEX IF EXISTS idx_games_external_id_sport;
DROP INDEX IF EXISTS idx_teams_abbreviation_sport;
DROP INDEX IF EXISTS idx_teams_external_id_sport;
DROP INDEX IF EXISTS idx_venues_external_id_sport;

-- Delete basketball data before restoring constraints.
DELETE FROM game_results WHERE game_id IN (SELECT id FROM games WHERE sport = 'basketball');
DELETE FROM money_line_odds WHERE game_id IN (SELECT id FROM games WHERE sport = 'basketball');
DELETE FROM spread_odds WHERE game_id IN (SELECT id FROM games WHERE sport = 'basketball');
DELETE FROM over_under_odds WHERE game_id IN (SELECT id FROM games WHERE sport = 'basketball');
DELETE FROM games WHERE sport = 'basketball';
DELETE FROM teams WHERE sport = 'basketball';
DELETE FROM venues WHERE sport = 'basketball';

-- Restore week_id NOT NULL.
ALTER TABLE games ALTER COLUMN week_id SET NOT NULL;

-- Restore original unique constraints.
ALTER TABLE venues ADD CONSTRAINT venues_external_id_key UNIQUE (external_id);
ALTER TABLE teams ADD CONSTRAINT teams_external_id_key UNIQUE (external_id);
ALTER TABLE teams ADD CONSTRAINT teams_abbreviation_key UNIQUE (abbreviation);
ALTER TABLE games ADD CONSTRAINT games_external_id_key UNIQUE (external_id);

-- Recreate original indexes.
CREATE INDEX IF NOT EXISTS idx_venues_external_id ON venues(external_id);
CREATE INDEX IF NOT EXISTS idx_teams_external_id ON teams(external_id);
CREATE INDEX IF NOT EXISTS idx_games_external_id ON games(external_id);

-- Drop new columns.
ALTER TABLE games DROP COLUMN IF EXISTS season_type;
ALTER TABLE games DROP COLUMN IF EXISTS season;
ALTER TABLE games DROP COLUMN IF EXISTS away_seed;
ALTER TABLE games DROP COLUMN IF EXISTS home_seed;
ALTER TABLE games DROP COLUMN IF EXISTS tournament;
ALTER TABLE games DROP COLUMN sport;
ALTER TABLE teams DROP COLUMN sport;
ALTER TABLE venues DROP COLUMN sport;

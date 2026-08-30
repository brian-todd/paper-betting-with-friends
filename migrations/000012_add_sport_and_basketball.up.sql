-- Add sport column to venues, teams, and games.
ALTER TABLE venues ADD COLUMN sport VARCHAR(20) NOT NULL DEFAULT 'football';
ALTER TABLE teams ADD COLUMN sport VARCHAR(20) NOT NULL DEFAULT 'football';
ALTER TABLE games ADD COLUMN sport VARCHAR(20) NOT NULL DEFAULT 'football';

-- Add basketball-specific columns to games.
ALTER TABLE games ADD COLUMN tournament VARCHAR(255);
ALTER TABLE games ADD COLUMN home_seed INTEGER;
ALTER TABLE games ADD COLUMN away_seed INTEGER;
ALTER TABLE games ADD COLUMN season INTEGER;
ALTER TABLE games ADD COLUMN season_type VARCHAR(20) DEFAULT 'regular';

-- Backfill games.season and games.season_type from the weeks join.
UPDATE games SET season = w.season, season_type = w.season_type
FROM weeks w WHERE games.week_id = w.id;

-- Make week_id nullable (basketball games have no week).
ALTER TABLE games ALTER COLUMN week_id DROP NOT NULL;

-- Drop old unique constraints on external_id (created by ALTER TABLE ... UNIQUE).
ALTER TABLE venues DROP CONSTRAINT IF EXISTS venues_external_id_key;
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_external_id_key;
ALTER TABLE games DROP CONSTRAINT IF EXISTS games_external_id_key;

-- Drop old unique constraint on abbreviation (created by CREATE TABLE ... UNIQUE).
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_abbreviation_key;

-- Drop old indexes on external_id.
DROP INDEX IF EXISTS idx_venues_external_id;
DROP INDEX IF EXISTS idx_teams_external_id;
DROP INDEX IF EXISTS idx_games_external_id;

-- Recreate as composite unique indexes with sport.
CREATE UNIQUE INDEX idx_venues_external_id_sport ON venues(external_id, sport);
CREATE UNIQUE INDEX idx_teams_external_id_sport ON teams(external_id, sport);
CREATE UNIQUE INDEX idx_teams_abbreviation_sport ON teams(abbreviation, sport);
CREATE UNIQUE INDEX idx_games_external_id_sport ON games(external_id, sport);

-- Add indexes for basketball date-range and season queries.
CREATE INDEX idx_games_sport_scheduled ON games(sport, scheduled_at);
CREATE INDEX idx_games_sport_season ON games(sport, season);
CREATE INDEX idx_games_tournament ON games(tournament) WHERE tournament IS NOT NULL;

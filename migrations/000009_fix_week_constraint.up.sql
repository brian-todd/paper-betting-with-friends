-- Drop old unique constraint that doesn't include season_type.
-- Regular season week 1 and postseason week 1 are different weeks.
ALTER TABLE weeks DROP CONSTRAINT IF EXISTS weeks_season_number_key;

-- Create new unique index that includes season_type.
CREATE UNIQUE INDEX IF NOT EXISTS idx_weeks_season_number_type 
  ON weeks(season, number, season_type);

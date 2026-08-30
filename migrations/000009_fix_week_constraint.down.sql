-- Drop the new index with season_type.
DROP INDEX IF EXISTS idx_weeks_season_number_type;

-- Restore original unique constraint (may fail if duplicate season/number exist).
ALTER TABLE weeks ADD CONSTRAINT weeks_season_number_key UNIQUE (season, number);

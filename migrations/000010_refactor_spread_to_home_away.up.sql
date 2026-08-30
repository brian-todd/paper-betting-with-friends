-- Add new home_spread and away_spread columns
ALTER TABLE spread_odds ADD COLUMN home_spread DECIMAL(5,1);
ALTER TABLE spread_odds ADD COLUMN away_spread DECIMAL(5,1);

-- Migrate existing data: home_spread = spread, away_spread = -spread
UPDATE spread_odds SET home_spread = spread, away_spread = -spread;

-- Make columns NOT NULL after data migration
ALTER TABLE spread_odds ALTER COLUMN home_spread SET NOT NULL;
ALTER TABLE spread_odds ALTER COLUMN away_spread SET NOT NULL;

-- Drop old spread column
ALTER TABLE spread_odds DROP COLUMN spread;

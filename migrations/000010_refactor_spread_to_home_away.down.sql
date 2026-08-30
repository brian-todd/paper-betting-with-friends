-- Add back the spread column
ALTER TABLE spread_odds ADD COLUMN spread DECIMAL(5,1);

-- Migrate data back: spread = home_spread
UPDATE spread_odds SET spread = home_spread;

-- Make column NOT NULL after data migration
ALTER TABLE spread_odds ALTER COLUMN spread SET NOT NULL;

-- Drop new columns
ALTER TABLE spread_odds DROP COLUMN home_spread;
ALTER TABLE spread_odds DROP COLUMN away_spread;

-- Drop purses table.
DROP TABLE IF EXISTS purses;

-- Remove starting_balance column from leagues table.
ALTER TABLE leagues DROP COLUMN IF EXISTS starting_balance;

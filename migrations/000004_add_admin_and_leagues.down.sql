-- Drop indexes.
DROP INDEX IF EXISTS idx_league_members_user_id;
DROP INDEX IF EXISTS idx_leagues_created_by;

-- Drop tables.
DROP TABLE IF EXISTS league_members;
DROP TABLE IF EXISTS leagues;

-- Remove is_admin column from users.
ALTER TABLE users DROP COLUMN IF EXISTS is_admin;

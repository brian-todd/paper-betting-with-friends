-- Remove visibility and invite code fields from leagues.
DROP INDEX IF EXISTS idx_leagues_invite_code;
ALTER TABLE leagues DROP COLUMN IF EXISTS invite_code;
ALTER TABLE leagues DROP COLUMN IF EXISTS is_public;

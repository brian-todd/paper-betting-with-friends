-- Add visibility and invite code fields to leagues.
ALTER TABLE leagues ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE leagues ADD COLUMN invite_code VARCHAR(16) UNIQUE;

-- Index for invite code lookups.
CREATE INDEX IF NOT EXISTS idx_leagues_invite_code ON leagues(invite_code);

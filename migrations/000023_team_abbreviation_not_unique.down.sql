-- Restore the unique index.
--
-- This will fail on any database that has since stored two teams sharing an
-- abbreviation, which is every database the up migration was worth running on.
-- That is the correct outcome for this rollback: the alternative is choosing
-- which of the colliding teams to delete, and a migration is the wrong place to
-- decide that.
DROP INDEX IF EXISTS idx_teams_abbreviation_sport;
CREATE UNIQUE INDEX idx_teams_abbreviation_sport ON teams(abbreviation, sport);

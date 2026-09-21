-- Restore the unique index.
--
-- This will fail on any database that has since stored two teams sharing an
-- abbreviation, which is every database the up migration was worth running on.
-- That is the correct outcome for this rollback: the alternative is choosing
-- which of the colliding teams to delete, and a migration is the wrong place to
-- decide that.
--
-- It does mean `make migrate-down` is a trap here. golang-migrate marks the
-- schema dirty on a failed migration and the server then refuses to boot, so
-- recovering is:
--
--   migrate -path migrations -database "$DATABASE_URL" force 23
--
-- which restores the version without re-running anything -- the index was already
-- dropped and recreated non-unique by the up migration, so the schema is in fact
-- correct for version 23 and only the dirty flag is wrong. Rolling back past this
-- point means deciding which of each colliding pair of teams to delete first.
DROP INDEX IF EXISTS idx_teams_abbreviation_sport;
CREATE UNIQUE INDEX idx_teams_abbreviation_sport ON teams(abbreviation, sport);

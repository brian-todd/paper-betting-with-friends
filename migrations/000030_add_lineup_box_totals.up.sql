-- The rest of what /lineups/game reports for a unit, as counts so a season can be
-- summed and its rates worked out from the sums.
--
-- Existing rows have none of these and cannot be given them, so they are cleared
-- along with the record of having asked. Games inside the lineups job's lookback are
-- fetched again; older ones are not. Nothing has run this table in production yet,
-- which is what makes that cheap.
DELETE FROM game_lineups;
DELETE FROM game_lineup_fetches;

ALTER TABLE game_lineups
    ADD COLUMN for_fg_made INTEGER NOT NULL,
    ADD COLUMN for_fg_attempted INTEGER NOT NULL,
    ADD COLUMN for_three_made INTEGER NOT NULL,
    ADD COLUMN for_three_attempted INTEGER NOT NULL,
    ADD COLUMN for_ft_made INTEGER NOT NULL,
    ADD COLUMN for_ft_attempted INTEGER NOT NULL,
    ADD COLUMN for_turnovers INTEGER NOT NULL,
    ADD COLUMN for_off_rebounds INTEGER NOT NULL,
    ADD COLUMN for_def_rebounds INTEGER NOT NULL,
    ADD COLUMN against_fg_made INTEGER NOT NULL,
    ADD COLUMN against_fg_attempted INTEGER NOT NULL,
    ADD COLUMN against_three_made INTEGER NOT NULL,
    ADD COLUMN against_three_attempted INTEGER NOT NULL,
    ADD COLUMN against_ft_made INTEGER NOT NULL,
    ADD COLUMN against_ft_attempted INTEGER NOT NULL,
    ADD COLUMN against_turnovers INTEGER NOT NULL,
    ADD COLUMN against_off_rebounds INTEGER NOT NULL,
    ADD COLUMN against_def_rebounds INTEGER NOT NULL;

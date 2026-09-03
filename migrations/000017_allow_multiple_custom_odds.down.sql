-- Restoring the unbounded unique index only succeeds while no game has more
-- than one custom row. Once a second custom bet has been placed on any game the
-- rollback will fail here, and that is the honest outcome: the duplicate rows
-- are referenced by bets under ON DELETE RESTRICT, so there is no way back to a
-- schema that forbids them without destroying the bets that point at them.
DROP INDEX IF EXISTS idx_money_line_odds_game_source;
DROP INDEX IF EXISTS idx_spread_odds_game_source;
DROP INDEX IF EXISTS idx_over_under_odds_game_source;

CREATE UNIQUE INDEX idx_money_line_odds_game_source ON money_line_odds (game_id, source);
CREATE UNIQUE INDEX idx_spread_odds_game_source ON spread_odds (game_id, source);
CREATE UNIQUE INDEX idx_over_under_odds_game_source ON over_under_odds (game_id, source);

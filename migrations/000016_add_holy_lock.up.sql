-- Holy Lock is a per-user, per-league, per-football-week marker on one pending
-- bet. It is display only: no stake, odds, payout or purse figure reads it, and
-- EvaluateBetsForGame and the admin status corrections are untouched.
--
-- The flag lives on the bet rows rather than in a table of its own because a
-- bet's week is derived (bets.game_id -> games.week_id -> weeks) and nothing in
-- this schema stores a bet's week directly. A holy_locks table would have to
-- denormalize the week to be indexable, and would then disagree with the weekly
-- breakdown as soon as the schedule sync moved a game to another week -- which
-- it does: GameRepository.Upsert lists week_id among its DoUpdates.
--
-- Adding a NOT NULL column with a constant DEFAULT is metadata-only on
-- PostgreSQL 11+, so this does not rewrite the bet tables.
ALTER TABLE spread_bets     ADD COLUMN is_holy_lock BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE money_line_bets ADD COLUMN is_holy_lock BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE over_under_bets ADD COLUMN is_holy_lock BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial indexes: every read asks which bets are locks, never the complement,
-- and the locks are a few rows per user per season against every bet ever
-- placed. A full index on the boolean would be almost entirely false rows.
CREATE INDEX IF NOT EXISTS idx_spread_bets_holy_lock
    ON spread_bets (user_id, league_id) WHERE is_holy_lock;
CREATE INDEX IF NOT EXISTS idx_money_line_bets_holy_lock
    ON money_line_bets (user_id, league_id) WHERE is_holy_lock;
CREATE INDEX IF NOT EXISTS idx_over_under_bets_holy_lock
    ON over_under_bets (user_id, league_id) WHERE is_holy_lock;

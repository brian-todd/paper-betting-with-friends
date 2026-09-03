-- Dropping the columns drops the partial indexes with them; they are named here
-- as well so a rollback reads explicitly rather than implicitly.
DROP INDEX IF EXISTS idx_over_under_bets_holy_lock;
DROP INDEX IF EXISTS idx_money_line_bets_holy_lock;
DROP INDEX IF EXISTS idx_spread_bets_holy_lock;

ALTER TABLE over_under_bets DROP COLUMN IF EXISTS is_holy_lock;
ALTER TABLE money_line_bets DROP COLUMN IF EXISTS is_holy_lock;
ALTER TABLE spread_bets     DROP COLUMN IF EXISTS is_holy_lock;

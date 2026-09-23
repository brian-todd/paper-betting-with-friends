-- How each sportsbook's line moved over time.
--
-- The odds tables hold one row per (game, source) and the lines sync overwrites
-- it in place, so they only ever know the current price. This table is the
-- history behind that row: one entry each time a series takes a value it did not
-- hold at the previous sync, and nothing when a sync finds the line unchanged.
--
-- Each row carries the whole value, not a delta from the last. The row count is
-- the same either way, but a delta log has to be summed from the first entry to
-- read any point in it, and one missing row corrupts every value after it
-- without anything noticing. Here the line at an instant is the latest row at or
-- before it, and a lost row costs one step.
--
-- A series is (game, market, source): each book prices each market on its own,
-- and that per-book history is the data. The columns are the odds tables' own,
-- set per market -- the CHECK holds each row to its market's shape. There is no
-- away_spread: a point spread is one number seen from both sides, and both syncs
-- write the away side as the negation of the home side.
--
-- recorded_at is the sync's clock, not now(), so a replayed fixture dates its
-- movements from the recording. The unique index gives a series one value per
-- instant, which is what makes "the latest row" a single row; a book quoted
-- twice in one response settles on the last quote, as the odds row does.
CREATE TABLE odds_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    source VARCHAR(50) NOT NULL,
    market VARCHAR(20) NOT NULL,

    home_spread DECIMAL(5,1),
    total DECIMAL(5,1),
    home_odds DECIMAL(10,2),
    away_odds DECIMAL(10,2),
    over_odds DECIMAL(10,2),
    under_odds DECIMAL(10,2),

    recorded_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT odds_movements_market_shape CHECK (
        (market = 'moneyline'
            AND home_odds IS NOT NULL AND away_odds IS NOT NULL
            AND home_spread IS NULL AND total IS NULL
            AND over_odds IS NULL AND under_odds IS NULL)
        OR (market = 'spread'
            AND home_spread IS NOT NULL AND home_odds IS NOT NULL AND away_odds IS NOT NULL
            AND total IS NULL AND over_odds IS NULL AND under_odds IS NULL)
        OR (market = 'total'
            AND total IS NOT NULL AND over_odds IS NOT NULL AND under_odds IS NOT NULL
            AND home_spread IS NULL AND home_odds IS NULL AND away_odds IS NULL)
    )
);

-- Serves both the recorder's "latest row in this series" and a game's history.
CREATE UNIQUE INDEX idx_odds_movements_series
    ON odds_movements (game_id, market, source, recorded_at);

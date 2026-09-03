-- One odds row per game per source is right for a book: DraftKings has one
-- spread on a game at a time, and the sync upserts onto that pair as the line
-- moves.
--
-- It is wrong for 'custom'. A custom line is written per bet, from the bettor's
-- own numbers, and frozen there -- so two people entering their own line on the
-- same game, or one person betting a game twice, is ordinary use. Under the old
-- index the second of them failed with a duplicate key error and the bet was
-- refused outright.
--
-- Excluding custom from the uniqueness keeps the book invariant the sync relies
-- on. The upserts name the same predicate so this partial index can still serve
-- as their ON CONFLICT arbiter.
DROP INDEX IF EXISTS idx_money_line_odds_game_source;
DROP INDEX IF EXISTS idx_spread_odds_game_source;
DROP INDEX IF EXISTS idx_over_under_odds_game_source;

CREATE UNIQUE INDEX idx_money_line_odds_game_source
    ON money_line_odds (game_id, source) WHERE source <> 'custom';
CREATE UNIQUE INDEX idx_spread_odds_game_source
    ON spread_odds (game_id, source) WHERE source <> 'custom';
CREATE UNIQUE INDEX idx_over_under_odds_game_source
    ON over_under_odds (game_id, source) WHERE source <> 'custom';

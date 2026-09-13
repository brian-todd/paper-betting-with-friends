-- Phase 2 of the pre-game stats spec: the two resources that are about betting
-- rather than about football.

-- Against-the-spread records, one row per (team, season), overwritten in place.
--
-- This is the most directly relevant table in the whole API for this app: a
-- team's rating says who is better, its ATS record says who has been beating
-- the number, and the number is what a bet is actually against.
--
-- Coverage is broader than the ratings -- 254 rows for 2026 against 138 -- so an
-- FCS side usually has one. It is not every team, though: a team nobody has ever
-- posted a line on has no ATS record to have.
CREATE TABLE team_ats_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    games INT NOT NULL DEFAULT 0,
    ats_wins INT NOT NULL DEFAULT 0,
    ats_losses INT NOT NULL DEFAULT 0,
    -- A push is a covered-by-exactly-zero game. Stored separately rather than
    -- folded into losses because the record reads "6-4-1" and a bettor who
    -- pushed got their stake back.
    ats_pushes INT NOT NULL DEFAULT 0,

    -- Two decimal places because the feed publishes two: a full 2025 season
    -- runs to -12.17 and 12.94, and early in a season a single game puts it at
    -- +-36.5. Six digits is far more headroom than either needs.
    avg_cover_margin DECIMAL(6,2),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season)
);

-- Pre-game win probability, one row per game.
--
-- Keyed on the game rather than the team because that is how the feed publishes
-- it, and because unlike everything else in this spec it describes a matchup
-- rather than a side.
--
-- Coverage tracks the market, not the schedule: the metric is derived from the
-- spread, so it exists for a game the moment somebody posts a line on it and
-- not before. In week 2 of 2026 that was 120 of the week's games, 15 of week
-- 3's, and single figures for most weeks after. A game without a row renders
-- nothing -- there is no fallback to compute, and inventing one would be
-- publishing a house model under a sourced metric's name.
CREATE TABLE game_pregame_win_probabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    fetched_at TIMESTAMPTZ NOT NULL,

    -- Home-relative, as published. Three decimal places because the feed gives
    -- three, and the range runs to a genuine 1.000.
    home_win_probability DECIMAL(4,3) NOT NULL,

    -- The spread the probability was derived from, in the feed's home-relative
    -- convention: negative means the home side is favoured. DECIMAL(5,1)
    -- matches spread_odds.spread, which holds the same kind of number.
    --
    -- Worth storing even though spread_odds exists: this is the line the metric
    -- used, which is not necessarily any line we hold, and a probability
    -- shown beside a spread it was not computed from is worse than no spread.
    spread DECIMAL(5,1) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id)
);

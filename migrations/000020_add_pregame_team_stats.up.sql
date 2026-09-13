-- Pre-game team context for the game detail page: ratings, season records, and
-- the pregame Elo the /games feed has always carried and we have always dropped.

-- /games reports a pregame Elo for both sides of every game, including ones not
-- yet played, so this costs no additional request. It is the only rating here
-- that is per-game rather than per-team-season.
ALTER TABLE games ADD COLUMN home_pregame_elo INT;
ALTER TABLE games ADD COLUMN away_pregame_elo INT;

-- Team ratings, one row per (team, season, source), overwritten in place.
--
-- These are decision support for choosing a bet, so only the current value
-- matters; nothing here reconstructs what a rating was before a game that has
-- since been played. fetched_at exists to render an "as of" line, not to
-- version the row -- no query selects on it.
--
-- The three sources are not interchangeable and are never averaged: SP+ and FPI
-- are points above an average team, CORE is opponent-relative efficiency on its
-- own scale.
CREATE TABLE team_ratings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INT NOT NULL,
    source VARCHAR(16) NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    -- Three decimal places because FPI publishes them (-14.484, 30.264) where
    -- SP+ rounds to one. Two would round every FPI rating on the way in.
    overall DECIMAL(6,3) NOT NULL,
    offense DECIMAL(6,3),
    defense DECIMAL(6,3),
    special_teams DECIMAL(6,3),

    overall_rank INT,
    offense_rank INT,
    defense_rank INT,

    -- FPI's resume ranks. Ranks, not ratings, and no other source supplies them.
    strength_of_schedule INT,
    strength_of_record INT,

    -- Only CORE reports what it has seen. Left null elsewhere rather than
    -- derived, so a reported week is never confused with a guessed one.
    through_week INT,
    model_version VARCHAR(64),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season, source)
);

-- Season records, one row per (team, season), overwritten in place.
--
-- /records also returns conference, neutral-site, regular-season and postseason
-- splits. Only the three with somewhere to go on the page are stored: overall
-- for the comparison row, and home/away for the side each applies to.
CREATE TABLE team_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    games INT NOT NULL DEFAULT 0,
    wins INT NOT NULL DEFAULT 0,
    losses INT NOT NULL DEFAULT 0,
    ties INT NOT NULL DEFAULT 0,

    home_games INT NOT NULL DEFAULT 0,
    home_wins INT NOT NULL DEFAULT 0,
    home_losses INT NOT NULL DEFAULT 0,
    home_ties INT NOT NULL DEFAULT 0,

    away_games INT NOT NULL DEFAULT 0,
    away_wins INT NOT NULL DEFAULT 0,
    away_losses INT NOT NULL DEFAULT 0,
    away_ties INT NOT NULL DEFAULT 0,

    expected_wins DECIMAL(4,2),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season)
);

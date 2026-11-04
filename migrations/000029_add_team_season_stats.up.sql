-- Basketball season figures for the game page's efficiency panel, one row per
-- (team, season), overwritten in place like team_ratings and for the same reason.
--
-- A table of its own rather than team_advanced_stats, whose columns are football's:
-- plays, drives, line yards, havoc.
--
-- Every figure is nullable, because the feed reports nulls for a team with no
-- tracked possessions, and a zero would read as the worst in the country. Percent
-- figures are all on a 0-100 scale, whatever scale the feed used.
CREATE TABLE team_season_stats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    games INT NOT NULL,
    pace DECIMAL(6,3),

    -- Points per 100 possessions, before any adjustment for the opposition.
    offense_rating DECIMAL(6,3),
    defense_rating DECIMAL(6,3),

    offense_effective_fg_pct DECIMAL(6,3),
    defense_effective_fg_pct DECIMAL(6,3),
    offense_turnover_pct DECIMAL(6,3),
    defense_turnover_pct DECIMAL(6,3),
    offense_rebound_pct DECIMAL(6,3),
    defense_rebound_pct DECIMAL(6,3),
    offense_free_throw_rate DECIMAL(6,3),
    defense_free_throw_rate DECIMAL(6,3),
    offense_three_pct DECIMAL(6,3),
    defense_three_pct DECIMAL(6,3),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season)
);

-- Phase 3 of the pre-game stats spec: the numbers behind a rating, and the
-- conditions the game will be played in.

-- Season efficiency, one row per (team, season), overwritten in place.
--
-- /stats/season/advanced returns roughly forty numbers per side. These sixteen
-- are the ones the panel explains a rating with; the rest -- standard-down and
-- passing-down splits, second-level and open-field yards, the running totals
-- behind every rate -- have no home on this page and are a migration away if
-- one appears.
--
-- This is deliberately not another rating. SP+ already prices these; the panel's
-- job here is to say *why* a team is rated where it is, which is why the rows
-- are named after what happens on the field rather than after a model.
CREATE TABLE team_advanced_stats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    -- Sample size. A team eight plays into a season has an explosiveness
    -- number and no business being read off one.
    offense_plays INT,
    offense_drives INT,
    defense_plays INT,
    defense_drives INT,

    -- Every rate and per-play figure is nullable. A team that has not run the
    -- ball has no line yards, and a zero would read as the worst in the
    -- country rather than as an absence.
    --
    -- Three decimal places throughout: the feed returns full float precision
    -- (0.2411451715382133) and nothing on the page shows more than two, but
    -- rounding on the way in is the lossy write this codebase avoids.
    offense_ppa DECIMAL(6,3),
    offense_success_rate DECIMAL(6,3),
    offense_explosiveness DECIMAL(6,3),
    offense_line_yards DECIMAL(6,3),
    offense_points_per_opportunity DECIMAL(6,3),
    offense_havoc DECIMAL(6,3),

    defense_ppa DECIMAL(6,3),
    defense_success_rate DECIMAL(6,3),
    defense_explosiveness DECIMAL(6,3),
    defense_line_yards DECIMAL(6,3),
    defense_points_per_opportunity DECIMAL(6,3),
    defense_havoc DECIMAL(6,3),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, season)
);

-- Kickoff forecast, one row per game.
--
-- games.live_state already carries weather, but only for games the scoreboard
-- covers, and the scoreboard covers the current week -- so there is no weather
-- for a game four days out, which is exactly when a bet gets placed. This table
-- is the pre-game half; the live strip stays the single source once a game
-- starts, and nothing here is rendered after kickoff.
--
-- indoors is the load-bearing column and the reason it is NOT NULL. A domed
-- stadium does not return an empty forecast -- it returns the weather outside
-- the roof, fully populated and entirely plausible. The 2025 season has 70
-- indoor games and all 70 carry a wind speed. Rendering one would be stating a
-- fact about a game that is false.
--
-- Pressure and dew point are not stored: pressure uses 0 for unknown (91 rows
-- in 2025) and neither tells a bettor anything temperature, wind and
-- precipitation do not.
CREATE TABLE game_forecasts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    fetched_at TIMESTAMPTZ NOT NULL,

    indoors BOOLEAN NOT NULL DEFAULT FALSE,

    temperature DECIMAL(5,1),
    humidity INT,
    -- Three decimal places: 2025 runs to 0.343 inches of rain and 0.157 of snow,
    -- and one place would round a light shower to nothing at all.
    precipitation DECIMAL(5,3),
    snowfall DECIMAL(5,3),
    wind_speed DECIMAL(5,1),

    -- The text label is absent more often than it is present for a forecast --
    -- 47 of 75 week-3 rows had none a week out, against all 86 of the current
    -- week's -- and the code is 0 in exactly those cases. The panel therefore
    -- renders from temperature and wind, and treats the label as a bonus.
    weather_condition VARCHAR(64),
    weather_condition_code INT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id)
);

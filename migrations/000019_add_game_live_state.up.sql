-- Live game state from the CFBD /scoreboard feed.
--
-- The score itself is not here: it belongs in game_results, which already owns
-- the provisional/final distinction that bet settlement reads. This table holds
-- everything else the scoreboard reports and no other feed does -- the clock,
-- the down and distance, the broadcast and the weather -- so a row's presence
-- means "the scoreboard has seen this game", not "this game is in progress".
--
-- One row per game, keyed on game_id rather than a surrogate, because the
-- scoreboard reports one state per game and nothing references a state.
CREATE TABLE game_live_states (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL UNIQUE REFERENCES games(id) ON DELETE CASCADE,

    -- Period and clock are the game clock as the provider last saw it. Both are
    -- null outside a live game, and stay set on a finished one -- read
    -- games.status to decide whether the clock is still running.
    period INT,
    clock VARCHAR(16),

    -- Situation is the down and distance, possession the side with the ball as
    -- the feed words it. Neither is parsed; they are provider prose.
    situation TEXT,
    possession VARCHAR(16),
    last_play TEXT,

    -- Broadcaster, as a display string. It can name several networks at once
    -- ("ESPN | Disney+"), so it is not an enum and not split.
    tv VARCHAR(255),

    weather_description VARCHAR(255),
    temperature DECIMAL(5,1),
    wind_speed DECIMAL(5,1),
    wind_direction INT,

    -- Live win probability for the home side. The feed reports it per team but
    -- the two are complementary, so one column carries both.
    home_win_probability DECIMAL(5,4),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

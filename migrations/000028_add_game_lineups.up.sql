-- Five-man unit totals from games already played, one row per game, team and unit.
-- Only what the page reads is kept: ratings are derived from these sums, which is
-- also what lets a team's season total be added up across games.
CREATE TABLE game_lineups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    id_hash VARCHAR(128) NOT NULL,
    athletes JSONB NOT NULL,
    seconds INTEGER NOT NULL,
    points_for INTEGER NOT NULL,
    possessions_for INTEGER NOT NULL,
    points_against INTEGER NOT NULL,
    possessions_against INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id, team_id, id_hash)
);

CREATE INDEX idx_game_lineups_team ON game_lineups (team_id);

-- Which games have been asked for, because an empty response is not an answer: it
-- can mean not processed yet or never available, so attempts are counted and bounded.
CREATE TABLE game_lineup_fetches (
    game_id UUID PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    attempts INTEGER NOT NULL,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    found_at TIMESTAMPTZ
);

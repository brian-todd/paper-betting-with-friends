-- Restores the tables as 000028 and 000030 left them, but not their rows. The job that
-- filled them is gone too, so they stay empty.
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
    for_fg_made INTEGER NOT NULL,
    for_fg_attempted INTEGER NOT NULL,
    for_three_made INTEGER NOT NULL,
    for_three_attempted INTEGER NOT NULL,
    for_ft_made INTEGER NOT NULL,
    for_ft_attempted INTEGER NOT NULL,
    for_turnovers INTEGER NOT NULL,
    for_off_rebounds INTEGER NOT NULL,
    for_def_rebounds INTEGER NOT NULL,
    against_fg_made INTEGER NOT NULL,
    against_fg_attempted INTEGER NOT NULL,
    against_three_made INTEGER NOT NULL,
    against_three_attempted INTEGER NOT NULL,
    against_ft_made INTEGER NOT NULL,
    against_ft_attempted INTEGER NOT NULL,
    against_turnovers INTEGER NOT NULL,
    against_off_rebounds INTEGER NOT NULL,
    against_def_rebounds INTEGER NOT NULL,
    UNIQUE (game_id, team_id, id_hash)
);

CREATE INDEX idx_game_lineups_team ON game_lineups (team_id);

CREATE TABLE game_lineup_fetches (
    game_id UUID PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    attempts INTEGER NOT NULL,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    found_at TIMESTAMPTZ
);

-- The same counts for a team's season and for one game, so the game page can show the
-- same rows before and after tip-off. Nullable because the feeds report nulls, and a
-- zero would read as a team that never scored. Existing season rows fill in on the next
-- cbb-team-stats run.
ALTER TABLE team_season_stats
    ADD COLUMN points INTEGER,
    ADD COLUMN fg_made INTEGER,
    ADD COLUMN fg_attempted INTEGER,
    ADD COLUMN three_made INTEGER,
    ADD COLUMN three_attempted INTEGER,
    ADD COLUMN ft_made INTEGER,
    ADD COLUMN ft_attempted INTEGER,
    ADD COLUMN rebounds INTEGER,
    ADD COLUMN off_rebounds INTEGER,
    ADD COLUMN def_rebounds INTEGER,
    ADD COLUMN assists INTEGER,
    ADD COLUMN steals INTEGER,
    ADD COLUMN blocks INTEGER,
    ADD COLUMN turnovers INTEGER,
    ADD COLUMN fouls INTEGER,
    ADD COLUMN paint_points INTEGER,
    ADD COLUMN fast_break_points INTEGER,
    ADD COLUMN points_off_turnovers INTEGER;

-- One row per game and team, written only once the game is final.
CREATE TABLE game_team_stats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    fetched_at TIMESTAMPTZ NOT NULL,
    points INTEGER,
    fg_made INTEGER,
    fg_attempted INTEGER,
    three_made INTEGER,
    three_attempted INTEGER,
    ft_made INTEGER,
    ft_attempted INTEGER,
    rebounds INTEGER,
    off_rebounds INTEGER,
    def_rebounds INTEGER,
    assists INTEGER,
    steals INTEGER,
    blocks INTEGER,
    turnovers INTEGER,
    fouls INTEGER,
    paint_points INTEGER,
    fast_break_points INTEGER,
    points_off_turnovers INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id, team_id)
);

CREATE INDEX idx_game_team_stats_team ON game_team_stats (team_id);

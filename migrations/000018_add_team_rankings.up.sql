-- Poll rankings, one row per (week, team, poll). Keyed on week_id rather than
-- (season, week, season_type) so the ranked-team filter is a one-line
-- correlated subquery against games.week_id instead of a join through weeks.
--
-- Every poll the feed returns is stored; which one counts as "ranked" for a
-- given week (CFP committee rankings when available, else AP Top 25) is
-- resolved by RankingRepository.EffectiveRanks, not baked into this table.
CREATE TABLE team_rankings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    week_id UUID NOT NULL REFERENCES weeks(id) ON DELETE CASCADE,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    poll VARCHAR(64) NOT NULL,
    rank INT NOT NULL,
    first_place_votes INT,
    points INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (week_id, team_id, poll)
);
CREATE INDEX idx_team_rankings_week_poll ON team_rankings (week_id, poll);

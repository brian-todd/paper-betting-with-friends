-- Basketball poll positions, one row per team, poll and poll date. Football's
-- team_rankings hangs off week_id, and basketball has no weeks, so a game finds
-- its rank by date instead: the latest poll on or before tip-off.
CREATE TABLE poll_ranks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    season INTEGER NOT NULL,
    poll VARCHAR(64) NOT NULL,
    poll_date TIMESTAMPTZ NOT NULL,
    rank INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, poll, poll_date)
);

CREATE INDEX idx_poll_ranks_season_poll_date ON poll_ranks (season, poll, poll_date);

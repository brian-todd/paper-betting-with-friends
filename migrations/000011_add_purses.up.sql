-- Add starting_balance column to leagues table.
ALTER TABLE leagues ADD COLUMN starting_balance DECIMAL(12,2) NOT NULL DEFAULT 1000;

-- Create purses table.
CREATE TABLE IF NOT EXISTS purses (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    league_id UUID NOT NULL REFERENCES leagues(id) ON DELETE CASCADE,
    balance DECIMAL(12,2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, league_id)
);

CREATE INDEX IF NOT EXISTS idx_purses_user_id ON purses(user_id);
CREATE INDEX IF NOT EXISTS idx_purses_league_id ON purses(league_id);

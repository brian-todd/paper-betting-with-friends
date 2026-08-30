-- Create venues table.
CREATE TABLE IF NOT EXISTS venues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    city VARCHAR(100) NOT NULL,
    state VARCHAR(50) NOT NULL,
    capacity INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create teams table.
CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    abbreviation VARCHAR(10) NOT NULL UNIQUE,
    conference VARCHAR(100) NOT NULL,
    home_venue_id UUID NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_teams_home_venue_id ON teams(home_venue_id);
CREATE INDEX IF NOT EXISTS idx_teams_conference ON teams(conference);

-- Create weeks table.
CREATE TABLE IF NOT EXISTS weeks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    season INTEGER NOT NULL,
    number INTEGER NOT NULL,
    start_date TIMESTAMP WITH TIME ZONE NOT NULL,
    end_date TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season, number)
);

CREATE INDEX IF NOT EXISTS idx_weeks_season ON weeks(season);

-- Create games table.
CREATE TABLE IF NOT EXISTS games (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    home_team_id UUID NOT NULL REFERENCES teams(id) ON DELETE RESTRICT,
    away_team_id UUID NOT NULL REFERENCES teams(id) ON DELETE RESTRICT,
    venue_id UUID NOT NULL REFERENCES venues(id) ON DELETE RESTRICT,
    week_id UUID NOT NULL REFERENCES weeks(id) ON DELETE RESTRICT,
    scheduled_at TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'scheduled',
    home_score INTEGER,
    away_score INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_games_home_team_id ON games(home_team_id);
CREATE INDEX IF NOT EXISTS idx_games_away_team_id ON games(away_team_id);
CREATE INDEX IF NOT EXISTS idx_games_venue_id ON games(venue_id);
CREATE INDEX IF NOT EXISTS idx_games_week_id ON games(week_id);
CREATE INDEX IF NOT EXISTS idx_games_scheduled_at ON games(scheduled_at);

-- Create money_line_odds table.
CREATE TABLE IF NOT EXISTS money_line_odds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    source VARCHAR(50) NOT NULL,
    home_odds DECIMAL(10,2) NOT NULL,
    away_odds DECIMAL(10,2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_money_line_odds_game_id ON money_line_odds(game_id);

-- Create spread_odds table.
CREATE TABLE IF NOT EXISTS spread_odds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    source VARCHAR(50) NOT NULL,
    spread DECIMAL(5,1) NOT NULL,
    home_odds DECIMAL(10,2) NOT NULL,
    away_odds DECIMAL(10,2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_spread_odds_game_id ON spread_odds(game_id);

-- Create over_under_odds table.
CREATE TABLE IF NOT EXISTS over_under_odds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    source VARCHAR(50) NOT NULL,
    total DECIMAL(5,1) NOT NULL,
    over_odds DECIMAL(10,2) NOT NULL,
    under_odds DECIMAL(10,2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_over_under_odds_game_id ON over_under_odds(game_id);

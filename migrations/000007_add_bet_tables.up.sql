-- Create money_line_bets table.
CREATE TABLE IF NOT EXISTS money_line_bets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    league_id UUID NOT NULL REFERENCES leagues(id) ON DELETE CASCADE,
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE RESTRICT,
    money_line_odds_id UUID NOT NULL REFERENCES money_line_odds(id) ON DELETE RESTRICT,
    pick VARCHAR(10) NOT NULL,
    odds_snapshot DECIMAL(10,2) NOT NULL,
    stake DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_money_line_bets_pick CHECK (pick IN ('home', 'away'))
);

CREATE INDEX IF NOT EXISTS idx_money_line_bets_user_id ON money_line_bets(user_id);
CREATE INDEX IF NOT EXISTS idx_money_line_bets_league_id ON money_line_bets(league_id);
CREATE INDEX IF NOT EXISTS idx_money_line_bets_game_id ON money_line_bets(game_id);
CREATE INDEX IF NOT EXISTS idx_money_line_bets_money_line_odds_id ON money_line_bets(money_line_odds_id);
CREATE INDEX IF NOT EXISTS idx_money_line_bets_status ON money_line_bets(status);

-- Create spread_bets table.
CREATE TABLE IF NOT EXISTS spread_bets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    league_id UUID NOT NULL REFERENCES leagues(id) ON DELETE CASCADE,
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE RESTRICT,
    spread_odds_id UUID NOT NULL REFERENCES spread_odds(id) ON DELETE RESTRICT,
    pick VARCHAR(10) NOT NULL,
    spread_snapshot DECIMAL(5,1) NOT NULL,
    odds_snapshot DECIMAL(10,2) NOT NULL,
    stake DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_spread_bets_pick CHECK (pick IN ('home', 'away'))
);

CREATE INDEX IF NOT EXISTS idx_spread_bets_user_id ON spread_bets(user_id);
CREATE INDEX IF NOT EXISTS idx_spread_bets_league_id ON spread_bets(league_id);
CREATE INDEX IF NOT EXISTS idx_spread_bets_game_id ON spread_bets(game_id);
CREATE INDEX IF NOT EXISTS idx_spread_bets_spread_odds_id ON spread_bets(spread_odds_id);
CREATE INDEX IF NOT EXISTS idx_spread_bets_status ON spread_bets(status);

-- Create over_under_bets table.
CREATE TABLE IF NOT EXISTS over_under_bets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    league_id UUID NOT NULL REFERENCES leagues(id) ON DELETE CASCADE,
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE RESTRICT,
    over_under_odds_id UUID NOT NULL REFERENCES over_under_odds(id) ON DELETE RESTRICT,
    pick VARCHAR(10) NOT NULL,
    total_snapshot DECIMAL(5,1) NOT NULL,
    odds_snapshot DECIMAL(10,2) NOT NULL,
    stake DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_over_under_bets_pick CHECK (pick IN ('over', 'under'))
);

CREATE INDEX IF NOT EXISTS idx_over_under_bets_user_id ON over_under_bets(user_id);
CREATE INDEX IF NOT EXISTS idx_over_under_bets_league_id ON over_under_bets(league_id);
CREATE INDEX IF NOT EXISTS idx_over_under_bets_game_id ON over_under_bets(game_id);
CREATE INDEX IF NOT EXISTS idx_over_under_bets_over_under_odds_id ON over_under_bets(over_under_odds_id);
CREATE INDEX IF NOT EXISTS idx_over_under_bets_status ON over_under_bets(status);

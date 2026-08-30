-- Create sessions table for server-side session storage (optional, for future use).
-- Currently using cookie-based sessions with gorilla/sessions.
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) NOT NULL UNIQUE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for token lookups.
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);

-- Index for user lookups.
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

-- Index for cleanup of expired sessions.
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

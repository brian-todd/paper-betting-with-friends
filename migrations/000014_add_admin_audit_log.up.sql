-- Record of every mutation made through the admin portal. Password resets,
-- purse edits and forced bet settlements are otherwise invisible after the
-- fact, which is exactly when someone asks what happened to their balance.
CREATE TABLE IF NOT EXISTS admin_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The actor may later be deleted; the row keeps a snapshot of the username
    -- so the history stays readable rather than becoming a list of nulls.
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_username VARCHAR(50) NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id UUID,
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_log_created_at ON admin_audit_log(created_at DESC);

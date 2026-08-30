-- Sessions are cookie-based and carry only a user id, so a password reset could
-- not previously revoke anything: an attacker's cookie stayed valid for the
-- remaining seven days. The cookie now carries this counter too, and a mismatch
-- is treated as unauthenticated.
ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 0;

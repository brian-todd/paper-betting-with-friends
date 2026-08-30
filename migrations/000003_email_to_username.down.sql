-- Revert username column back to email.
ALTER TABLE users RENAME COLUMN username TO email;
ALTER INDEX idx_users_username RENAME TO idx_users_email;

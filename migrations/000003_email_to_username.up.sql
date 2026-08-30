-- Rename email column to username.
ALTER TABLE users RENAME COLUMN email TO username;
ALTER INDEX idx_users_email RENAME TO idx_users_username;

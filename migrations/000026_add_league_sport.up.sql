-- A league plays one sport, fixed when it is created, with its own purses and
-- standings. Every league before this one was made for football.
ALTER TABLE leagues ADD COLUMN sport VARCHAR(20) NOT NULL DEFAULT 'football';

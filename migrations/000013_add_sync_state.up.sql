-- Records when each background sync last succeeded.
--
-- The scheduler tracks this in memory, which is lost on every restart and
-- deploy. That leaves the footer claiming the data has never been refreshed
-- when it was refreshed minutes ago, and it is worst exactly when it matters
-- most: right after a deploy, when someone is looking to see whether the sync
-- is alive.
--
-- One row per job, and no history. This answers "how old is the data" and
-- nothing else; the run-by-run record lives in the logs.
CREATE TABLE sync_state (
    job             TEXT PRIMARY KEY,
    last_success_at TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

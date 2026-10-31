-- The judge session's counters: fresh selfies started and render seats
-- taken, per identity session. A cap is a count, not a quota of open
-- sessions. Rows are keyed by session id, so an erased session leaves a
-- dead row and no user data.
CREATE TABLE IF NOT EXISTS judge_sessions (
    session_id    TEXT    NOT NULL PRIMARY KEY,
    fresh_selfies INTEGER NOT NULL DEFAULT 0,
    renders       INTEGER NOT NULL DEFAULT 0
);
-- The style-history switch, one row per user who has set it.
-- No row means off.
CREATE TABLE history_switch (
    user_id TEXT    NOT NULL PRIMARY KEY,
    enabled INTEGER NOT NULL,
    CHECK (enabled IN (0, 1))
);

-- The snapshot a restarted look rebuilds its request from: the inputs of
-- one started look, no bytes. root_id is the job id the runner minted,
-- which is also the look's id, so the row leaves with its look. user_id
-- is the identity user id, and deletion walks this table like every
-- other: the snapshot carries the recall sentence, which is the user's.
CREATE TABLE look_snapshots (
    root_id  TEXT NOT NULL PRIMARY KEY REFERENCES looks (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL,
    snapshot TEXT NOT NULL
);

CREATE INDEX look_snapshots_user_idx ON look_snapshots (user_id);
-- One attempt. selfie_id is the mediastore id of the normalised selfie,
-- not the photo. photo_hash is that selfie, and it is the analysis cache
-- key. user_id is the identity user id. This schema does not create that
-- table, and it does not reference it, so the file migrates on its own.
-- verdict is empty until the user keeps, dismisses or buys.
-- created_at is unix seconds.
CREATE TABLE looks (
    id         TEXT    NOT NULL PRIMARY KEY,
    user_id    TEXT    NOT NULL,
    selfie_id  TEXT    NOT NULL,
    photo_hash TEXT    NOT NULL,
    occasion   TEXT    NOT NULL,
    fix        TEXT    NOT NULL DEFAULT '',
    runner_up  TEXT    NOT NULL DEFAULT '',
    verdict    TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    CHECK (verdict IN ('', 'kept', 'dismissed', 'bought'))
);

CREATE INDEX looks_user_idx ON looks (user_id, created_at);

-- One flagged criterion, in the order the judge returned.
CREATE TABLE look_flags (
    look_id      TEXT    NOT NULL REFERENCES looks (id) ON DELETE CASCADE,
    user_id      TEXT    NOT NULL,
    position     INTEGER NOT NULL,
    criterion_id TEXT    NOT NULL,
    PRIMARY KEY (look_id, criterion_id),
    UNIQUE (look_id, position)
);

CREATE INDEX look_flags_user_idx ON look_flags (user_id);

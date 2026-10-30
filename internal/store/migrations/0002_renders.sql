-- A YouCam result copied into mediastore. media_id is that copy.
-- A YouCam link is not stored; those links expire. discarded stays 1
-- when the check rejected the render, and the row stays too, so deletion
-- can still find the copy.
CREATE TABLE renders (
    id        TEXT    NOT NULL PRIMARY KEY,
    look_id   TEXT    NOT NULL REFERENCES looks (id) ON DELETE CASCADE,
    user_id   TEXT    NOT NULL,
    media_id  TEXT    NOT NULL,
    fix_id    TEXT    NOT NULL,
    position  INTEGER NOT NULL,
    discarded INTEGER NOT NULL DEFAULT 0,
    UNIQUE (look_id, position),
    CHECK (discarded IN (0, 1))
);

CREATE INDEX renders_user_idx ON renders (user_id);
CREATE INDEX renders_look_idx ON renders (look_id);

-- Clef's check of one render. The row is absent until Clef has answered.
CREATE TABLE render_checks (
    render_id     TEXT    NOT NULL PRIMARY KEY REFERENCES renders (id) ON DELETE CASCADE,
    look_id       TEXT    NOT NULL REFERENCES looks (id) ON DELETE CASCADE,
    user_id       TEXT    NOT NULL,
    change_made   INTEGER NOT NULL,
    same_person   INTEGER NOT NULL,
    criterion_met INTEGER NOT NULL,
    CHECK (change_made IN (0, 1)),
    CHECK (same_person IN (0, 1)),
    CHECK (criterion_met IN (0, 1))
);

CREATE INDEX render_checks_user_idx ON render_checks (user_id);
CREATE INDEX render_checks_look_idx ON render_checks (look_id);

-- One vocabulary answer on a check.
CREATE TABLE render_check_attributes (
    render_id    TEXT NOT NULL REFERENCES render_checks (render_id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL,
    attribute_id TEXT NOT NULL,
    value        TEXT NOT NULL,
    PRIMARY KEY (render_id, attribute_id)
);

CREATE INDEX render_check_attributes_user_idx ON render_check_attributes (user_id);

-- One YouCam read of one user's normalised photo.
-- The cache key is photo_hash. user_id scopes it to the owner, so deletion
-- finds the row and two users do not share a face reading. task is which
-- read, so a later look does not pay again for a read already stored.
-- The value columns are the picker allowlist and nothing else. Age, gender,
-- ratios and scores have no column. An empty value means that read did not
-- yield the attribute. The row existing means the read is cached.
CREATE TABLE analysis_cache (
    user_id      TEXT NOT NULL,
    photo_hash   TEXT NOT NULL,
    task         TEXT NOT NULL,
    face_shape   TEXT NOT NULL DEFAULT '',
    hair_type    TEXT NOT NULL DEFAULT '',
    hair_length  TEXT NOT NULL DEFAULT '',
    colour_tones TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, photo_hash, task)
);

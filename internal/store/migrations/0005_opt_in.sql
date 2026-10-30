-- One fix the user has ticked for one occasion.
-- No row means that fix is not open. Nothing here infers the rest of the menu.
CREATE TABLE opt_ins (
    user_id  TEXT NOT NULL,
    occasion TEXT NOT NULL,
    fix_id   TEXT NOT NULL,
    PRIMARY KEY (user_id, occasion, fix_id)
);

-- An item a user brought. The catalog loaded from testdata is not copied
-- here. fidelity is the label shown on the product.
CREATE TABLE products (
    id          TEXT NOT NULL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    category    TEXT NOT NULL,
    name        TEXT NOT NULL,
    fidelity    TEXT NOT NULL,
    origin      TEXT NOT NULL,
    youcam_role TEXT NOT NULL DEFAULT '',
    photograph  TEXT NOT NULL DEFAULT '',
    CHECK (fidelity IN ('exact product', 'approximate match', 'visual simulation')),
    CHECK (origin IN ('catalog', 'user'))
);

CREATE INDEX products_user_idx ON products (user_id);

-- The labelled products shown on one look, in shop order.
-- product_id may be a catalog id or a products id. A catalog id has no row
-- in products, because the catalog is not user data.
CREATE TABLE look_products (
    look_id     TEXT    NOT NULL REFERENCES looks (id) ON DELETE CASCADE,
    user_id     TEXT    NOT NULL,
    position    INTEGER NOT NULL,
    product_id  TEXT    NOT NULL,
    category    TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    fidelity    TEXT    NOT NULL,
    origin      TEXT    NOT NULL,
    youcam_role TEXT    NOT NULL DEFAULT '',
    photograph  TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (look_id, position),
    CHECK (fidelity IN ('exact product', 'approximate match', 'visual simulation')),
    CHECK (origin IN ('catalog', 'user'))
);

CREATE INDEX look_products_user_idx ON look_products (user_id);

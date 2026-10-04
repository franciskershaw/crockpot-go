CREATE TABLE
    IF NOT EXISTS regular_items (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        item_id UUID NOT NULL REFERENCES items (id) ON DELETE RESTRICT,
        unit_id UUID REFERENCES units (id) ON DELETE RESTRICT,
        quantity NUMERIC(10, 2) NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (user_id, item_id)
    );

CREATE INDEX IF NOT EXISTS idx_regular_items_item_id ON regular_items (item_id);

CREATE INDEX IF NOT EXISTS idx_regular_items_unit_id ON regular_items (unit_id);

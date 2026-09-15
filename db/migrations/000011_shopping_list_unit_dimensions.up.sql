ALTER TABLE units
    ADD COLUMN dimension TEXT NOT NULL DEFAULT 'count',
    ADD COLUMN base_factor NUMERIC(10, 4) NOT NULL DEFAULT 1;

ALTER TABLE units
    ADD CONSTRAINT units_dimension_check CHECK (dimension IN ('mass', 'volume', 'count'));

UPDATE units SET dimension = 'mass', base_factor = 1 WHERE name = 'grams';
UPDATE units SET dimension = 'mass', base_factor = 1000 WHERE name = 'kilogram';
UPDATE units SET dimension = 'volume', base_factor = 1 WHERE name = 'milliliters';
UPDATE units SET dimension = 'volume', base_factor = 1000 WHERE name = 'litres';
UPDATE units SET dimension = 'volume', base_factor = 15 WHERE name = 'tablespoons';
UPDATE units SET dimension = 'volume', base_factor = 5 WHERE name = 'teaspoons';
UPDATE units SET dimension = 'volume', base_factor = 250 WHERE name = 'cup';
UPDATE units SET dimension = 'volume', base_factor = 568 WHERE name = 'pint';

CREATE TABLE
    IF NOT EXISTS shopping_list_dismissed_items (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        shopping_list_id UUID NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
        item_id UUID NOT NULL REFERENCES items (id) ON DELETE CASCADE,
        unit_id UUID REFERENCES units (id) ON DELETE CASCADE,
        quantity_at_dismissal NUMERIC(10, 2) NOT NULL
    );

CREATE INDEX IF NOT EXISTS idx_shopping_list_dismissed_items_shopping_list_id ON shopping_list_dismissed_items (shopping_list_id);
CREATE INDEX IF NOT EXISTS idx_shopping_list_dismissed_items_item_id ON shopping_list_dismissed_items (item_id);
CREATE INDEX IF NOT EXISTS idx_shopping_list_dismissed_items_unit_id ON shopping_list_dismissed_items (unit_id);

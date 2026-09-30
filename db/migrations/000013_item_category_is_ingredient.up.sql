ALTER TABLE item_categories
    ADD COLUMN is_ingredient BOOLEAN NOT NULL DEFAULT true;

UPDATE item_categories SET is_ingredient = false WHERE name = 'House';

DROP TABLE IF EXISTS shopping_list_dismissed_items;

ALTER TABLE units
    DROP CONSTRAINT units_dimension_check;

ALTER TABLE units
    DROP COLUMN dimension,
    DROP COLUMN base_factor;

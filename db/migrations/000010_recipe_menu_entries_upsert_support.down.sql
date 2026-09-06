ALTER TABLE recipe_menu_entries
    DROP CONSTRAINT recipe_menu_entries_menu_recipe_key;

ALTER TABLE recipe_menu_entries
    DROP COLUMN created_at;

ALTER TABLE recipe_menu_entries
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE recipe_menu_entries
    ADD CONSTRAINT recipe_menu_entries_menu_recipe_key UNIQUE (recipe_menu_id, recipe_id);

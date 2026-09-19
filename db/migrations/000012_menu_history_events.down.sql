DROP TABLE IF EXISTS menu_history_events;

COMMENT ON TABLE menu_history_baseline IS NULL;

ALTER TABLE menu_history_baseline
    DROP CONSTRAINT menu_history_baseline_menu_recipe_key;

CREATE INDEX IF NOT EXISTS idx_menu_history_entries_recipe_menu_id ON menu_history_baseline (recipe_menu_id);

ALTER INDEX idx_menu_history_baseline_recipe_id RENAME TO idx_menu_history_entries_recipe_id;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_baseline_recipe_id_fkey TO menu_history_entries_recipe_id_fkey;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_baseline_recipe_menu_id_fkey TO menu_history_entries_recipe_menu_id_fkey;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_baseline_pkey TO menu_history_entries_pkey;

ALTER TABLE menu_history_baseline RENAME TO menu_history_entries;

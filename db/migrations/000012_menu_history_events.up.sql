ALTER TABLE menu_history_entries RENAME TO menu_history_baseline;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_entries_pkey TO menu_history_baseline_pkey;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_entries_recipe_menu_id_fkey TO menu_history_baseline_recipe_menu_id_fkey;

ALTER TABLE menu_history_baseline RENAME CONSTRAINT menu_history_entries_recipe_id_fkey TO menu_history_baseline_recipe_id_fkey;

ALTER INDEX idx_menu_history_entries_recipe_id RENAME TO idx_menu_history_baseline_recipe_id;

-- The unique constraint's index leads recipe_menu_id, so the single-column index is redundant.
DROP INDEX IF EXISTS idx_menu_history_entries_recipe_menu_id;

ALTER TABLE menu_history_baseline
    ADD CONSTRAINT menu_history_baseline_menu_recipe_key UNIQUE (recipe_menu_id, recipe_id);

COMMENT ON TABLE menu_history_baseline IS 'Frozen per-recipe aggregates migrated from the old app; never written by the API. times_added_to_menu is an upper bound: the old app counted serves-only changes as adds.';

CREATE TABLE
    IF NOT EXISTS menu_history_events (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        recipe_menu_id UUID NOT NULL REFERENCES recipe_menus (id) ON DELETE CASCADE,
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        event_type TEXT NOT NULL CHECK (event_type IN ('add', 'remove')),
        occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_menu_history_events_menu_recipe_occurred ON menu_history_events (recipe_menu_id, recipe_id, occurred_at);

CREATE INDEX IF NOT EXISTS idx_menu_history_events_recipe_id ON menu_history_events (recipe_id);

INSERT INTO
    menu_history_events (recipe_menu_id, recipe_id, event_type, occurred_at)
SELECT
    recipe_menu_id,
    recipe_id,
    'add',
    created_at
FROM
    recipe_menu_entries;

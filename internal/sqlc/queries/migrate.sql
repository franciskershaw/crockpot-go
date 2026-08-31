-- Queries for cmd/migrate-data only. Every column is explicit (id, timestamps,
-- created_by_name) so the one-off import can preserve the source's real values
-- instead of the API's server-assigned defaults. See docs/handoffs/CROC-024.md.

-- name: MigrateInsertUser :exec
INSERT INTO users (
    id, email, google_id, name, image, role, email_verified_at,
    created_at, updated_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(email), sqlc.arg(google_id), sqlc.narg(name),
    sqlc.narg(image), sqlc.arg(role), sqlc.narg(email_verified_at),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: MigrateInsertItem :exec
INSERT INTO items (id, name, category_id, created_at, updated_at)
VALUES (
    sqlc.arg(id), sqlc.arg(name), sqlc.arg(category_id),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: MigrateInsertItemAllowedUnit :exec
INSERT INTO item_allowed_units (item_id, unit_id)
VALUES (sqlc.arg(item_id), sqlc.arg(unit_id));

-- name: MigrateInsertRecipe :exec
INSERT INTO recipes (
    id, name, time_in_minutes, image_url, image_filename,
    instructions, notes, approved, serves,
    created_by_id, created_by_name, created_at, updated_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(name), sqlc.arg(time_in_minutes),
    sqlc.narg(image_url), sqlc.narg(image_filename),
    sqlc.arg(instructions), sqlc.arg(notes), sqlc.arg(approved), sqlc.arg(serves),
    sqlc.narg(created_by_id), sqlc.narg(created_by_name),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: MigrateInsertRecipeIngredient :exec
INSERT INTO recipe_ingredients (recipe_id, item_id, unit_id, quantity, position)
VALUES (
    sqlc.arg(recipe_id), sqlc.arg(item_id), sqlc.narg(unit_id),
    sqlc.arg(quantity), sqlc.arg(position)
);

-- name: MigrateInsertRecipeCategoryLink :exec
INSERT INTO recipe_categories_recipes (recipe_id, category_id)
VALUES (sqlc.arg(recipe_id), sqlc.arg(category_id));

-- name: MigrateTruncate :exec
-- Every table that FKs into recipes or items is named explicitly (no CASCADE)
-- so a future table added against those references fails this loudly instead of
-- being wiped silently. Keep in sync with 000001_init.up.sql.
TRUNCATE
    recipe_categories_recipes,
    recipe_ingredients,
    recipe_favourites,
    recipe_menu_entries,
    menu_history_entries,
    shopping_list_items,
    item_allowed_units,
    recipes,
    items
RESTART IDENTITY;

-- name: MigrateDeleteUsers :exec
DELETE FROM users
WHERE id = ANY(sqlc.arg(ids)::uuid[])
   OR google_id = ANY(sqlc.arg(google_ids)::text[]);

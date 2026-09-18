-- name: GetOrCreateMenu :one
INSERT INTO recipe_menus (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = excluded.user_id
RETURNING id;

-- name: GetMenuByUserID :one
SELECT id FROM recipe_menus WHERE user_id = $1;

-- name: UpsertMenuEntry :exec
INSERT INTO recipe_menu_entries (recipe_menu_id, recipe_id, serves)
VALUES ($1, $2, $3)
ON CONFLICT (recipe_menu_id, recipe_id) DO UPDATE SET serves = excluded.serves;

-- name: UpdateMenuEntryServes :execrows
UPDATE recipe_menu_entries
SET serves = $3
WHERE recipe_menu_id = $1 AND recipe_id = $2;

-- name: RemoveMenuEntry :exec
DELETE FROM recipe_menu_entries WHERE recipe_menu_id = $1 AND recipe_id = $2;

-- name: ClearMenuEntries :exec
DELETE FROM recipe_menu_entries
WHERE recipe_menu_id IN (SELECT id FROM recipe_menus WHERE user_id = sqlc.arg(user_id));

-- name: ListMenuEntries :many
SELECT rme.serves AS menu_serves, r.*
FROM recipe_menu_entries rme
JOIN recipes r ON r.id = rme.recipe_id
WHERE rme.recipe_menu_id = $1
ORDER BY rme.created_at DESC;

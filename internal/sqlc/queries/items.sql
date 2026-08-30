-- name: ListItems :many
SELECT * FROM items
ORDER BY name;

-- name: ListItemAllowedUnitIDsForItems :many
SELECT item_id, unit_id FROM item_allowed_units
WHERE item_id = ANY(sqlc.arg(item_ids)::uuid[])
ORDER BY unit_id;

-- name: ListItemAllowedUnitIDs :many
SELECT unit_id FROM item_allowed_units
WHERE item_id = $1
ORDER BY unit_id;

-- name: CreateItem :one
INSERT INTO items (name, category_id)
VALUES ($1, $2)
RETURNING *;

-- name: CreateItemAllowedUnit :exec
INSERT INTO item_allowed_units (item_id, unit_id)
VALUES ($1, $2);

-- name: DeleteItemAllowedUnitsForItem :exec
DELETE FROM item_allowed_units
WHERE item_id = $1;

-- name: UpdateItem :one
UPDATE items
SET name = COALESCE(NULLIF(sqlc.arg(name)::text, ''), name),
    category_id = COALESCE(sqlc.narg(category_id), category_id),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteItem :one
DELETE FROM items
WHERE id = $1
RETURNING id;

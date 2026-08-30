-- name: ListItemCategories :many
SELECT * FROM item_categories
ORDER BY name;

-- name: CreateItemCategory :one
INSERT INTO item_categories (name, icon)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateItemCategory :one
UPDATE item_categories
SET name = COALESCE(NULLIF(sqlc.arg(name)::text, ''), name),
    icon = COALESCE(NULLIF(sqlc.arg(icon)::text, ''), icon),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteItemCategory :one
DELETE FROM item_categories
WHERE id = $1
RETURNING id;

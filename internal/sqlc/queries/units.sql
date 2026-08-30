-- name: ListUnits :many
SELECT * FROM units
ORDER BY name;

-- name: CreateUnit :one
INSERT INTO units (name, abbreviation)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateUnit :one
UPDATE units
SET name = COALESCE(NULLIF(sqlc.arg(name)::text, ''), name),
    abbreviation = COALESCE(NULLIF(sqlc.arg(abbreviation)::text, ''), abbreviation),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUnit :one
DELETE FROM units
WHERE id = $1
RETURNING id;

-- name: ListRecipeCategories :many
SELECT * FROM recipe_categories
ORDER BY name;

-- name: CreateRecipeCategory :one
INSERT INTO recipe_categories (name)
VALUES ($1)
RETURNING *;

-- name: UpdateRecipeCategory :one
UPDATE recipe_categories
SET name = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: DeleteRecipeCategory :one
DELETE FROM recipe_categories
WHERE id = $1
RETURNING id;

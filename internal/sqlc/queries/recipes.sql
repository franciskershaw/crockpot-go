-- name: CreateRecipe :one
INSERT INTO recipes (
    name,
    time_in_minutes,
    serves,
    instructions,
    notes,
    image_url,
    image_filename,
    approved,
    created_by_id,
    created_by_name
)
VALUES (
    sqlc.arg(name),
    sqlc.arg(time_in_minutes),
    sqlc.arg(serves),
    sqlc.arg(instructions),
    sqlc.arg(notes),
    sqlc.narg(image_url),
    sqlc.narg(image_filename),
    sqlc.arg(approved),
    sqlc.arg(created_by_id),
    (SELECT name FROM users WHERE id = sqlc.arg(created_by_id))
)
RETURNING *;

-- name: CreateRecipeIngredient :exec
INSERT INTO recipe_ingredients (recipe_id, item_id, unit_id, quantity, position)
VALUES ($1, $2, $3, $4, $5);

-- name: CreateRecipeCategoryLink :exec
INSERT INTO recipe_categories_recipes (recipe_id, category_id)
VALUES ($1, $2);

-- name: CountRecipesByCreator :one
SELECT count(*) FROM recipes
WHERE created_by_id = $1;

-- name: ListRecipeIngredients :many
SELECT item_id, unit_id, quantity FROM recipe_ingredients
WHERE recipe_id = $1
ORDER BY position;

-- name: ListRecipeCategoryIDsForRecipe :many
SELECT category_id FROM recipe_categories_recipes
WHERE recipe_id = $1
ORDER BY category_id;

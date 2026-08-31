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

-- name: ListRecipes :many
SELECT r.*
FROM recipes r
WHERE (
        r.approved
        OR sqlc.arg(caller_is_admin)::boolean
        OR (sqlc.narg(caller_id)::uuid IS NOT NULL AND r.created_by_id = sqlc.narg(caller_id)::uuid)
    )
    AND (
        NOT sqlc.arg(only_mine)::boolean
        OR (sqlc.narg(caller_id)::uuid IS NOT NULL AND r.created_by_id = sqlc.narg(caller_id)::uuid)
    )
    AND (sqlc.arg(name_query)::text = '' OR r.name ILIKE '%' || sqlc.arg(name_query)::text || '%')
    AND (sqlc.arg(min_time)::int = 0 OR r.time_in_minutes >= sqlc.arg(min_time)::int)
    AND (sqlc.arg(max_time)::int = 0 OR r.time_in_minutes <= sqlc.arg(max_time)::int)
    AND (
        cardinality(sqlc.arg(exclude_category_ids)::uuid[]) = 0
        OR NOT EXISTS (
            SELECT 1 FROM recipe_categories_recipes x
            WHERE x.recipe_id = r.id AND x.category_id = ANY(sqlc.arg(exclude_category_ids)::uuid[])
        )
    )
    AND (
        (
            cardinality(sqlc.arg(include_category_ids)::uuid[]) = 0
            AND cardinality(sqlc.arg(ingredient_ids)::uuid[]) = 0
        )
        OR EXISTS (
            SELECT 1 FROM recipe_categories_recipes x
            WHERE x.recipe_id = r.id AND x.category_id = ANY(sqlc.arg(include_category_ids)::uuid[])
        )
        OR EXISTS (
            SELECT 1 FROM recipe_ingredients x
            WHERE x.recipe_id = r.id AND x.item_id = ANY(sqlc.arg(ingredient_ids)::uuid[])
        )
    )
ORDER BY r.created_at DESC, r.id
LIMIT sqlc.arg(result_limit)::int OFFSET sqlc.arg(result_offset)::int;

-- name: CountRecipes :one
SELECT count(*)
FROM recipes r
WHERE (
        r.approved
        OR sqlc.arg(caller_is_admin)::boolean
        OR (sqlc.narg(caller_id)::uuid IS NOT NULL AND r.created_by_id = sqlc.narg(caller_id)::uuid)
    )
    AND (
        NOT sqlc.arg(only_mine)::boolean
        OR (sqlc.narg(caller_id)::uuid IS NOT NULL AND r.created_by_id = sqlc.narg(caller_id)::uuid)
    )
    AND (sqlc.arg(name_query)::text = '' OR r.name ILIKE '%' || sqlc.arg(name_query)::text || '%')
    AND (sqlc.arg(min_time)::int = 0 OR r.time_in_minutes >= sqlc.arg(min_time)::int)
    AND (sqlc.arg(max_time)::int = 0 OR r.time_in_minutes <= sqlc.arg(max_time)::int)
    AND (
        cardinality(sqlc.arg(exclude_category_ids)::uuid[]) = 0
        OR NOT EXISTS (
            SELECT 1 FROM recipe_categories_recipes x
            WHERE x.recipe_id = r.id AND x.category_id = ANY(sqlc.arg(exclude_category_ids)::uuid[])
        )
    )
    AND (
        (
            cardinality(sqlc.arg(include_category_ids)::uuid[]) = 0
            AND cardinality(sqlc.arg(ingredient_ids)::uuid[]) = 0
        )
        OR EXISTS (
            SELECT 1 FROM recipe_categories_recipes x
            WHERE x.recipe_id = r.id AND x.category_id = ANY(sqlc.arg(include_category_ids)::uuid[])
        )
        OR EXISTS (
            SELECT 1 FROM recipe_ingredients x
            WHERE x.recipe_id = r.id AND x.item_id = ANY(sqlc.arg(ingredient_ids)::uuid[])
        )
    );

-- name: GetRecipeForReader :one
SELECT r.*
FROM recipes r
WHERE r.id = sqlc.arg(id)
    AND (
        r.approved
        OR sqlc.arg(caller_is_admin)::boolean
        OR (sqlc.narg(caller_id)::uuid IS NOT NULL AND r.created_by_id = sqlc.narg(caller_id)::uuid)
    );

-- name: ListRecipeCardCategories :many
SELECT rcr.recipe_id, rc.id, rc.name
FROM recipe_categories_recipes rcr
JOIN recipe_categories rc ON rc.id = rcr.category_id
WHERE rcr.recipe_id = ANY(sqlc.arg(recipe_ids)::uuid[])
ORDER BY rc.name;

-- name: ListRecipeDetailCategories :many
SELECT rc.id, rc.name
FROM recipe_categories_recipes rcr
JOIN recipe_categories rc ON rc.id = rcr.category_id
WHERE rcr.recipe_id = sqlc.arg(recipe_id)
ORDER BY rc.name;

-- name: ListRecipeIngredientsHydrated :many
SELECT
    ri.item_id,
    i.name AS item_name,
    i.category_id AS item_category_id,
    ic.name AS item_category_name,
    ri.unit_id,
    u.abbreviation AS unit_abbreviation,
    ri.quantity
FROM recipe_ingredients ri
JOIN items i ON i.id = ri.item_id
JOIN item_categories ic ON ic.id = i.category_id
LEFT JOIN units u ON u.id = ri.unit_id
WHERE ri.recipe_id = sqlc.arg(recipe_id)
ORDER BY ri.position;

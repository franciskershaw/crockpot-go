-- name: AddFavourite :exec
INSERT INTO recipe_favourites (user_id, recipe_id) VALUES ($1, $2)
ON CONFLICT (user_id, recipe_id) DO NOTHING;

-- name: RemoveFavourite :exec
DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2;

-- name: RecipeVisibleToCaller :one
SELECT EXISTS (
    SELECT 1 FROM recipes r
    WHERE r.id = $1
      AND (r.approved OR sqlc.arg(caller_is_admin)::boolean OR r.created_by_id = sqlc.arg(caller_id)::uuid)
);

-- name: ListFavouritedRecipeIDs :many
SELECT recipe_id FROM recipe_favourites
WHERE user_id = $1 AND recipe_id = ANY(sqlc.arg(recipe_ids)::uuid[]);

-- name: IsRecipeFavourited :one
SELECT EXISTS (
    SELECT 1 FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2
);

-- name: ListFavouriteRecipes :many
SELECT r.*
FROM recipes r
JOIN recipe_favourites f ON f.recipe_id = r.id
WHERE f.user_id = $1
ORDER BY f.created_at DESC, r.id
LIMIT $2 OFFSET $3;

-- name: CountFavouriteRecipes :one
SELECT count(*) FROM recipe_favourites WHERE user_id = $1;
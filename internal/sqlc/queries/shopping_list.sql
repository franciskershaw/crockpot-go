-- name: GetOrCreateShoppingList :one
INSERT INTO shopping_lists (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = excluded.user_id
RETURNING id;

-- name: GetShoppingListByUserID :one
SELECT id FROM shopping_lists WHERE user_id = $1;

-- name: ListShoppingListItemsHydrated :many
SELECT
    sli.id,
    sli.item_id,
    i.name AS item_name,
    i.category_id AS item_category_id,
    ic.name AS item_category_name,
    sli.unit_id,
    u.abbreviation AS unit_abbreviation,
    sli.quantity,
    sli.obtained,
    sli.is_manual
FROM shopping_list_items sli
JOIN items i ON i.id = sli.item_id
JOIN item_categories ic ON ic.id = i.category_id
LEFT JOIN units u ON u.id = sli.unit_id
WHERE sli.shopping_list_id = sqlc.arg(shopping_list_id)
ORDER BY ic.name, i.name;

-- name: AggregateMenuIngredients :many
WITH base_units AS (
    SELECT
        (SELECT id FROM units WHERE name = 'grams') AS grams_id,
        (SELECT id FROM units WHERE name = 'milliliters') AS milliliters_id
), expanded AS (
    SELECT
        ri.item_id,
        ri.unit_id,
        ri.quantity,
        (rme.serves::numeric / r.serves::numeric) AS scale,
        u.dimension,
        u.base_factor,
        CASE u.dimension
            WHEN 'mass' THEN base_units.grams_id
            WHEN 'volume' THEN base_units.milliliters_id
        END AS base_unit_id
    FROM recipe_menu_entries rme
    JOIN recipes r ON r.id = rme.recipe_id
    JOIN recipe_ingredients ri ON ri.recipe_id = r.id
    LEFT JOIN units u ON u.id = ri.unit_id
    CROSS JOIN base_units
    WHERE rme.recipe_menu_id = sqlc.arg(recipe_menu_id)
)
SELECT
    item_id,
    COALESCE(base_unit_id, unit_id)::uuid AS unit_id,
    SUM(quantity * scale * COALESCE(base_factor, 1))::numeric(10, 2) AS quantity
FROM expanded
GROUP BY item_id, COALESCE(base_unit_id, unit_id);

-- name: SyncShoppingListItemQuantities :exec
UPDATE shopping_list_items sli
SET quantity = agg.quantity
FROM (
    SELECT
        unnest(sqlc.arg(item_ids)::uuid[]) AS item_id,
        unnest(sqlc.arg(unit_ids)::uuid[]) AS unit_id,
        unnest(sqlc.arg(quantities)::numeric[]) AS quantity
) agg
WHERE sli.shopping_list_id = sqlc.arg(shopping_list_id)
    AND sli.item_id = agg.item_id
    AND sli.unit_id IS NOT DISTINCT FROM agg.unit_id
    AND NOT sli.is_manual;

-- name: InsertNewShoppingListItems :exec
INSERT INTO shopping_list_items (shopping_list_id, item_id, unit_id, quantity, obtained, is_manual)
SELECT sqlc.arg(shopping_list_id), agg.item_id, agg.unit_id, agg.quantity, false, false
FROM (
    SELECT
        unnest(sqlc.arg(item_ids)::uuid[]) AS item_id,
        unnest(sqlc.arg(unit_ids)::uuid[]) AS unit_id,
        unnest(sqlc.arg(quantities)::numeric[]) AS quantity
) agg
WHERE NOT EXISTS (
        SELECT 1 FROM shopping_list_items sli
        WHERE sli.shopping_list_id = sqlc.arg(shopping_list_id)
            AND sli.item_id = agg.item_id
            AND sli.unit_id IS NOT DISTINCT FROM agg.unit_id
            AND NOT sli.is_manual
    )
    AND NOT EXISTS (
        SELECT 1 FROM shopping_list_dismissed_items d
        WHERE d.shopping_list_id = sqlc.arg(shopping_list_id)
            AND d.item_id = agg.item_id
            AND d.unit_id IS NOT DISTINCT FROM agg.unit_id
            AND d.quantity_at_dismissal = agg.quantity
    );

-- name: DeleteStaleDismissals :exec
DELETE FROM shopping_list_dismissed_items d USING (
    SELECT
        unnest(sqlc.arg(item_ids)::uuid[]) AS item_id,
        unnest(sqlc.arg(unit_ids)::uuid[]) AS unit_id,
        unnest(sqlc.arg(quantities)::numeric[]) AS quantity
) agg
WHERE d.shopping_list_id = sqlc.arg(shopping_list_id)
    AND d.item_id = agg.item_id
    AND d.unit_id IS NOT DISTINCT FROM agg.unit_id
    AND d.quantity_at_dismissal <> agg.quantity;

-- name: FindManualShoppingListItem :one
SELECT id, quantity FROM shopping_list_items
WHERE shopping_list_id = sqlc.arg(shopping_list_id)
    AND item_id = sqlc.arg(item_id)
    AND unit_id IS NOT DISTINCT FROM sqlc.arg(unit_id)::uuid
    AND is_manual
LIMIT 1;

-- name: IncrementShoppingListItemQuantity :exec
UPDATE shopping_list_items
SET quantity = quantity + sqlc.arg(delta)
WHERE id = sqlc.arg(id);

-- name: InsertManualShoppingListItem :exec
INSERT INTO shopping_list_items (shopping_list_id, item_id, unit_id, quantity, obtained, is_manual)
VALUES (sqlc.arg(shopping_list_id), sqlc.arg(item_id), sqlc.arg(unit_id), sqlc.arg(quantity), false, true);

-- name: UpdateShoppingListItem :execrows
UPDATE shopping_list_items
SET obtained = COALESCE(sqlc.narg(obtained), obtained),
    quantity = COALESCE(sqlc.narg(quantity), quantity)
WHERE id = sqlc.arg(id) AND shopping_list_id = sqlc.arg(shopping_list_id);

-- name: DeleteObsoleteShoppingListItems :exec
DELETE FROM shopping_list_items sli
WHERE sli.shopping_list_id = sqlc.arg(shopping_list_id)
    AND NOT sli.is_manual
    AND NOT EXISTS (
        SELECT 1
        FROM (
            SELECT
                unnest(sqlc.arg(item_ids)::uuid[]) AS item_id,
                unnest(sqlc.arg(unit_ids)::uuid[]) AS unit_id
        ) t
        WHERE t.item_id = sli.item_id AND t.unit_id IS NOT DISTINCT FROM sli.unit_id
    );

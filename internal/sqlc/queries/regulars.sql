-- name: ListRegularsHydrated :many
SELECT
    r.id,
    r.item_id,
    i.name AS item_name,
    i.category_id,
    ic.name AS category_name,
    r.unit_id,
    u.abbreviation AS unit_abbreviation,
    r.quantity
FROM regular_items r
JOIN items i ON i.id = r.item_id
JOIN item_categories ic ON ic.id = i.category_id
LEFT JOIN units u ON u.id = r.unit_id
WHERE r.user_id = sqlc.arg(user_id)
ORDER BY ic.name, i.name;

-- name: GetRegularHydrated :one
SELECT
    r.id,
    r.item_id,
    i.name AS item_name,
    i.category_id,
    ic.name AS category_name,
    r.unit_id,
    u.abbreviation AS unit_abbreviation,
    r.quantity
FROM regular_items r
JOIN items i ON i.id = r.item_id
JOIN item_categories ic ON ic.id = i.category_id
LEFT JOIN units u ON u.id = r.unit_id
WHERE r.id = sqlc.arg(id) AND r.user_id = sqlc.arg(user_id);

-- name: InsertRegularWithinLimit :one
INSERT INTO regular_items (user_id, item_id, unit_id, quantity)
SELECT sqlc.arg(user_id), sqlc.arg(item_id), sqlc.narg(unit_id), sqlc.arg(quantity)
WHERE (SELECT count(*) FROM regular_items WHERE user_id = sqlc.arg(user_id)) < sqlc.arg(max_regulars)::bigint
RETURNING id;

-- name: GetRegularItemID :one
SELECT item_id FROM regular_items
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: UpdateRegular :execrows
UPDATE regular_items
SET unit_id = sqlc.narg(unit_id), quantity = sqlc.arg(quantity), updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: DeleteRegular :execrows
DELETE FROM regular_items
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

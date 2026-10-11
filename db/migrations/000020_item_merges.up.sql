CREATE TEMP TABLE item_merges ON COMMIT DROP AS
SELECT merged.id AS merged_id, kept.id AS kept_id
FROM (VALUES
    ('Five-spice mix', 'Chinese 5 spice'),
    ('Dill (chopped)', 'Dill'),
    ('Pepper (herb)', 'Black pepper')
) AS m (merged_name, kept_name)
JOIN items merged ON merged.name = m.merged_name
JOIN items kept ON kept.name = m.kept_name;

INSERT INTO item_allowed_units (item_id, unit_id)
SELECT m.kept_id, a.unit_id
FROM item_merges m
JOIN item_allowed_units a ON a.item_id = m.merged_id
ON CONFLICT DO NOTHING;

UPDATE recipe_ingredients ri
SET item_id = m.kept_id
FROM item_merges m
WHERE ri.item_id = m.merged_id;

UPDATE shopping_list_items kept_row
SET quantity = kept_row.quantity + merged_row.quantity
FROM shopping_list_items merged_row
JOIN item_merges m ON m.merged_id = merged_row.item_id
WHERE kept_row.shopping_list_id = merged_row.shopping_list_id
    AND kept_row.item_id = m.kept_id
    AND kept_row.unit_id IS NOT DISTINCT FROM merged_row.unit_id
    AND NOT kept_row.is_manual
    AND NOT merged_row.is_manual;

DELETE FROM shopping_list_items merged_row
USING item_merges m, shopping_list_items kept_row
WHERE merged_row.item_id = m.merged_id
    AND kept_row.shopping_list_id = merged_row.shopping_list_id
    AND kept_row.item_id = m.kept_id
    AND kept_row.unit_id IS NOT DISTINCT FROM merged_row.unit_id
    AND NOT kept_row.is_manual
    AND NOT merged_row.is_manual;

UPDATE shopping_list_items s
SET item_id = m.kept_id
FROM item_merges m
WHERE s.item_id = m.merged_id;

UPDATE shopping_list_dismissed_items d
SET item_id = m.kept_id
FROM item_merges m
WHERE d.item_id = m.merged_id;

DELETE FROM regular_items r
USING item_merges m
WHERE r.item_id = m.merged_id
    AND EXISTS (SELECT 1 FROM regular_items k WHERE k.user_id = r.user_id AND k.item_id = m.kept_id);

UPDATE regular_items r
SET item_id = m.kept_id, updated_at = CURRENT_TIMESTAMP
FROM item_merges m
WHERE r.item_id = m.merged_id;

DELETE FROM items i
USING item_merges m
WHERE i.id = m.merged_id;

DELETE FROM items i
WHERE i.name IN ('Steak', 'Washing Up Tablets')
    AND NOT EXISTS (SELECT 1 FROM recipe_ingredients x WHERE x.item_id = i.id)
    AND NOT EXISTS (SELECT 1 FROM shopping_list_items x WHERE x.item_id = i.id)
    AND NOT EXISTS (SELECT 1 FROM regular_items x WHERE x.item_id = i.id);

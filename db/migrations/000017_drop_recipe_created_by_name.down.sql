ALTER TABLE recipes ADD COLUMN created_by_name TEXT;

UPDATE recipes r
SET created_by_name = u.name
FROM users u
WHERE u.id = r.created_by_id;

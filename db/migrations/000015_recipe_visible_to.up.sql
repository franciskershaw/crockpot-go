-- COALESCE, not IS NOT DISTINCT FROM: a NULL caller must never match a deleted (NULL) creator.
CREATE OR REPLACE FUNCTION recipe_visible_to (
    approved BOOLEAN,
    created_by_id UUID,
    caller_id UUID,
    caller_is_admin BOOLEAN
) RETURNS BOOLEAN LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN approved
OR caller_is_admin
OR COALESCE(created_by_id = caller_id, false);

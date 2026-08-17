-- name: CreateRefreshTokenFamily :one
INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteStaleRefreshTokenFamiliesForUser :exec
DELETE FROM refresh_tokens
WHERE user_id = $1 AND (revoked_at IS NOT NULL OR expires_at < CURRENT_TIMESTAMP);

-- name: RevokeAllRefreshTokenFamiliesForUser :exec
UPDATE refresh_tokens
SET revoked_at = CURRENT_TIMESTAMP
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at >= CURRENT_TIMESTAMP;

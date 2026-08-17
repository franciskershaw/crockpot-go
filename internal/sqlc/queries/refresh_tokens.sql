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

-- name: GetRefreshTokenFamilyByID :one
SELECT * FROM refresh_tokens
WHERE id = $1 AND user_id = $2;

-- name: RotateRefreshTokenFamily :exec
UPDATE refresh_tokens
SET previous_token_hash = token_hash,
    previous_token_rotated_at = CURRENT_TIMESTAMP,
    token_hash = $2,
    expires_at = $3
WHERE id = $1;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET revoked_at = CURRENT_TIMESTAMP
WHERE id = $1;

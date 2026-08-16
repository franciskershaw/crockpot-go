-- name: CreateEmailVerificationToken :one
INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: FindActiveEmailVerificationTokenByUserID :one
SELECT * FROM email_verification_tokens
WHERE user_id = $1 AND used_at IS NULL;

-- name: IncrementEmailVerificationTokenAttempts :one
UPDATE email_verification_tokens
SET attempts = attempts + 1
WHERE id = $1
RETURNING *;

-- name: MarkEmailVerificationTokenUsed :exec
UPDATE email_verification_tokens
SET used_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: DeleteActiveEmailVerificationTokensForUser :exec
DELETE FROM email_verification_tokens
WHERE user_id = $1 AND used_at IS NULL;

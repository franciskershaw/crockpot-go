-- name: GetUserByGoogleID :one
SELECT * FROM users
WHERE google_id = $1;

-- name: CreateGoogleUser :one
INSERT INTO users (email, google_id, name, image, email_verified_at, last_login_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING *;

-- name: UpdateUserLoginProfile :one
UPDATE users
SET name = $2, image = $3, last_login_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

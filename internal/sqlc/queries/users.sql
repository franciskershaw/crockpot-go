-- name: GetUserByGoogleID :one
SELECT * FROM users
WHERE google_id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: CreateUnconfirmedUser :one
INSERT INTO users (email, password_hash, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: MarkUserEmailConfirmed :one
UPDATE users
SET email_verified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: CreateGoogleUser :one
INSERT INTO users (email, google_id, name, image, email_verified_at, last_login_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING *;

-- name: UpdateUserLoginProfile :one
UPDATE users
SET name = COALESCE(NULLIF(sqlc.arg(display_name)::text, ''), name),
    image = COALESCE(NULLIF(sqlc.arg(avatar_url)::text, ''), image),
    last_login_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateUserLastLogin :one
UPDATE users
SET last_login_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateUserPassword :one
UPDATE users
SET password_hash = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;
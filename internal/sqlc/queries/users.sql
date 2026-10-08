-- name: GetUserByGoogleID :one
SELECT * FROM users
WHERE google_id = $1;

-- name: GetUserByIDForUpdate :one
SELECT * FROM users
WHERE id = $1
FOR UPDATE;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

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
INSERT INTO users (email, google_id, name, email_verified_at, last_login_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
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

-- name: UpdateUserName :one
UPDATE users
SET name = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetUserName :one
SELECT name FROM users
WHERE id = $1;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = $1;

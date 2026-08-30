package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgConstraintError maps a constraint-violation error to a domain error by constraint name — unique per table, so matching on name alone (regardless of SQLSTATE class) is unambiguous.
func pgConstraintError(err error, byConstraint map[string]error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	return byConstraint[pgErr.ConstraintName]
}

// pgErrorCode reports whether err is a pgconn.PgError with the given SQLSTATE code.
func pgErrorCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

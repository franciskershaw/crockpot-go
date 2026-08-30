package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PostgresUnitRepository struct {
	db sqlc.DBTX
}

func NewPostgresUnitRepository(db sqlc.DBTX) *PostgresUnitRepository {
	return &PostgresUnitRepository{db: db}
}

func (r *PostgresUnitRepository) List(ctx context.Context) ([]*models.Unit, error) {
	rows, err := queriesFor(ctx, r.db).ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list units: %w", err)
	}
	units := make([]*models.Unit, len(rows))
	for i, row := range rows {
		units[i] = toModelUnit(row)
	}
	return units, nil
}

func (r *PostgresUnitRepository) Create(ctx context.Context, name, abbreviation string) (*models.Unit, error) {
	created, err := queriesFor(ctx, r.db).CreateUnit(ctx, sqlc.CreateUnitParams{
		Name:         name,
		Abbreviation: abbreviation,
	})
	if err != nil {
		if constraintErr := unitConstraintError(err); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to create unit: %w", err)
	}
	return toModelUnit(created), nil
}

func (r *PostgresUnitRepository) Update(ctx context.Context, id, name, abbreviation string) (*models.Unit, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid unit id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).UpdateUnit(ctx, sqlc.UpdateUnitParams{
		Name:         name,
		Abbreviation: abbreviation,
		ID:           idUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUnitNotFound
		}
		if constraintErr := unitConstraintError(err); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to update unit: %w", err)
	}
	return toModelUnit(updated), nil
}

func (r *PostgresUnitRepository) Delete(ctx context.Context, id string) error {
	idUUID, err := uuidParam(id)
	if err != nil {
		return fmt.Errorf("invalid unit id: %w", err)
	}
	_, err = queriesFor(ctx, r.db).DeleteUnit(ctx, idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrUnitNotFound
		}
		var pgErr *pgconn.PgError
		// 23001 (restrict_violation) from recipe_ingredients/shopping_list_items (both RESTRICT);
		// item_allowed_units is CASCADE and deliberately not caught here.
		if errors.As(err, &pgErr) && pgErr.Code == "23001" {
			return models.ErrUnitInUse
		}
		return fmt.Errorf("failed to delete unit: %w", err)
	}
	return nil
}

// unitConstraintError maps a 23505 unique-violation to its domain error by constraint name, or returns nil for any other error.
func unitConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil
	}
	switch pgErr.ConstraintName {
	case "units_name_key":
		return models.ErrUnitNameTaken
	case "units_abbreviation_key":
		return models.ErrUnitAbbreviationTaken
	default:
		return nil
	}
}

func toModelUnit(u sqlc.Unit) *models.Unit {
	return &models.Unit{
		ID:           uuidValue(u.ID),
		Name:         u.Name,
		Abbreviation: u.Abbreviation,
		CreatedAt:    u.CreatedAt.Time,
		UpdatedAt:    u.UpdatedAt.Time,
	}
}

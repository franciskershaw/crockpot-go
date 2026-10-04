package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var regularConstraintErrors = map[string]error{
	"regular_items_user_id_item_id_key": models.ErrRegularExists,
	"regular_items_item_id_fkey":        models.ErrRegularInvalidItem,
	"regular_items_unit_id_fkey":        models.ErrRegularInvalidUnit,
}

type PostgresRegularRepository struct {
	db sqlc.DBTX
}

func NewPostgresRegularRepository(db sqlc.DBTX) *PostgresRegularRepository {
	return &PostgresRegularRepository{db: db}
}

func (r *PostgresRegularRepository) List(ctx context.Context, userID string) ([]models.Regular, error) {
	uid, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	rows, err := queriesFor(ctx, r.db).ListRegularsHydrated(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to list regulars: %w", err)
	}

	regulars := make([]models.Regular, len(rows))
	for i, row := range rows {
		regular, err := regularFromRow(row)
		if err != nil {
			return nil, err
		}
		regulars[i] = *regular
	}
	return regulars, nil
}

// Create refuses once the user already has limit regulars; the count and insert are one statement but not serialised.
func (r *PostgresRegularRepository) Create(ctx context.Context, userID, itemID string, unitID *string, quantity float64, limit int) (*models.Regular, error) {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	iid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, fmt.Errorf("invalid item id: %w", err)
	}
	pgUnitID, err := regularUnitParam(ctx, q, iid, unitID)
	if err != nil {
		return nil, err
	}
	qty, err := numericParam(quantity)
	if err != nil {
		return nil, fmt.Errorf("invalid quantity: %w", err)
	}

	id, err := q.InsertRegularWithinLimit(ctx, sqlc.InsertRegularWithinLimitParams{
		UserID:      uid,
		ItemID:      pgUUID(iid),
		UnitID:      pgUnitID,
		Quantity:    qty,
		MaxRegulars: int64(limit),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrRegularsLimitReached
	}
	if err != nil {
		if mapped := pgConstraintError(err, regularConstraintErrors); mapped != nil {
			return nil, mapped
		}
		return nil, fmt.Errorf("failed to insert regular: %w", err)
	}

	return r.get(ctx, q, id, uid)
}

// Update replaces both unit and quantity; a nil unitID clears the unit.
func (r *PostgresRegularRepository) Update(ctx context.Context, userID, regularID string, unitID *string, quantity float64) (*models.Regular, error) {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(regularID)
	if err != nil {
		return nil, fmt.Errorf("invalid regular id: %w", err)
	}

	itemID, err := q.GetRegularItemID(ctx, sqlc.GetRegularItemIDParams{ID: rid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrRegularNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get regular: %w", err)
	}

	pgUnitID, err := regularUnitParam(ctx, q, uuidValue(itemID), unitID)
	if err != nil {
		return nil, err
	}
	qty, err := numericParam(quantity)
	if err != nil {
		return nil, fmt.Errorf("invalid quantity: %w", err)
	}

	affected, err := q.UpdateRegular(ctx, sqlc.UpdateRegularParams{
		UnitID:   pgUnitID,
		Quantity: qty,
		ID:       rid,
		UserID:   uid,
	})
	if err != nil {
		if mapped := pgConstraintError(err, regularConstraintErrors); mapped != nil {
			return nil, mapped
		}
		return nil, fmt.Errorf("failed to update regular: %w", err)
	}
	if affected == 0 {
		return nil, models.ErrRegularNotFound
	}

	return r.get(ctx, q, rid, uid)
}

func (r *PostgresRegularRepository) Delete(ctx context.Context, userID, regularID string) error {
	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(regularID)
	if err != nil {
		return fmt.Errorf("invalid regular id: %w", err)
	}

	affected, err := queriesFor(ctx, r.db).DeleteRegular(ctx, sqlc.DeleteRegularParams{ID: rid, UserID: uid})
	if err != nil {
		return fmt.Errorf("failed to delete regular: %w", err)
	}
	if affected == 0 {
		return models.ErrRegularNotFound
	}
	return nil
}

// regularUnitParam parses unitID and rejects a unit outside the item's allowed set.
func regularUnitParam(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID, unitID *string) (pgtype.UUID, error) {
	if unitID == nil {
		return pgtype.UUID{}, nil
	}
	parsed, err := uuid.Parse(*unitID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid unit id: %w", err)
	}
	if err := checkAllowedUnits(ctx, q, []models.Ingredient{{ItemID: itemID, UnitID: &parsed}}); err != nil {
		return pgtype.UUID{}, err
	}
	return pgUUID(parsed), nil
}

func (r *PostgresRegularRepository) get(ctx context.Context, q *sqlc.Queries, id, userID pgtype.UUID) (*models.Regular, error) {
	row, err := q.GetRegularHydrated(ctx, sqlc.GetRegularHydratedParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrRegularNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get regular: %w", err)
	}
	return regularFromRow(sqlc.ListRegularsHydratedRow(row))
}

func regularFromRow(row sqlc.ListRegularsHydratedRow) (*models.Regular, error) {
	qty, err := numericValue(row.Quantity)
	if err != nil {
		return nil, fmt.Errorf("failed to read regular quantity: %w", err)
	}
	regular := &models.Regular{
		ID:               uuidValue(row.ID),
		ItemID:           uuidValue(row.ItemID),
		ItemName:         row.ItemName,
		CategoryID:       uuidValue(row.CategoryID),
		CategoryName:     row.CategoryName,
		UnitAbbreviation: textPtr(row.UnitAbbreviation),
		Quantity:         qty,
	}
	if row.UnitID.Valid {
		u := uuidValue(row.UnitID)
		regular.UnitID = &u
	}
	return regular, nil
}

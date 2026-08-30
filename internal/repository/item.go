package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var itemWriteConstraintErrors = map[string]error{
	"items_name_key":         models.ErrItemNameTaken,
	"items_category_id_fkey": models.ErrItemInvalidCategory,
}

var itemAllowedUnitConstraintErrors = map[string]error{
	"item_allowed_units_unit_id_fkey": models.ErrItemInvalidUnit,
}

type PostgresItemRepository struct {
	db sqlc.DBTX
}

func NewPostgresItemRepository(db sqlc.DBTX) *PostgresItemRepository {
	return &PostgresItemRepository{db: db}
}

func (r *PostgresItemRepository) List(ctx context.Context) ([]*models.Item, error) {
	rows, err := queriesFor(ctx, r.db).ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list items: %w", err)
	}

	itemIDs := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		itemIDs[i] = row.ID
	}
	joinRows, err := queriesFor(ctx, r.db).ListItemAllowedUnitIDsForItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list item allowed units: %w", err)
	}
	allowedByItem := make(map[uuid.UUID][]uuid.UUID, len(rows))
	for _, join := range joinRows {
		itemID := uuidValue(join.ItemID)
		allowedByItem[itemID] = append(allowedByItem[itemID], uuidValue(join.UnitID))
	}

	items := make([]*models.Item, len(rows))
	for i, row := range rows {
		items[i] = toModelItem(row, allowedByItem[uuidValue(row.ID)])
	}
	return items, nil
}

func (r *PostgresItemRepository) Create(ctx context.Context, name, categoryID string, unitIDs []string) (*models.Item, error) {
	categoryUUID, err := uuidParam(categoryID)
	if err != nil {
		return nil, fmt.Errorf("invalid category id: %w", err)
	}

	created, err := queriesFor(ctx, r.db).CreateItem(ctx, sqlc.CreateItemParams{
		Name:       name,
		CategoryID: categoryUUID,
	})
	if err != nil {
		if constraintErr := pgConstraintError(err, itemWriteConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to create item: %w", err)
	}

	allowedUnitIDs, err := r.insertAllowedUnits(ctx, created.ID, unitIDs)
	if err != nil {
		return nil, err
	}

	return toModelItem(created, allowedUnitIDs), nil
}

func (r *PostgresItemRepository) Update(ctx context.Context, id, name, categoryID string, unitIDs []string) (*models.Item, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid item id: %w", err)
	}

	categoryParam := pgtype.UUID{Valid: false}
	if categoryID != "" {
		categoryParam, err = uuidParam(categoryID)
		if err != nil {
			return nil, fmt.Errorf("invalid category id: %w", err)
		}
	}

	updated, err := queriesFor(ctx, r.db).UpdateItem(ctx, sqlc.UpdateItemParams{
		Name:       name,
		CategoryID: categoryParam,
		ID:         idUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrItemNotFound
		}
		if constraintErr := pgConstraintError(err, itemWriteConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to update item: %w", err)
	}

	var allowedUnitIDs []uuid.UUID
	if unitIDs != nil {
		if err := queriesFor(ctx, r.db).DeleteItemAllowedUnitsForItem(ctx, idUUID); err != nil {
			return nil, fmt.Errorf("failed to clear item allowed units: %w", err)
		}
		allowedUnitIDs, err = r.insertAllowedUnits(ctx, idUUID, unitIDs)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err := queriesFor(ctx, r.db).ListItemAllowedUnitIDs(ctx, idUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to list item allowed units: %w", err)
		}
		allowedUnitIDs = make([]uuid.UUID, len(rows))
		for i, unitID := range rows {
			allowedUnitIDs[i] = uuidValue(unitID)
		}
	}

	return toModelItem(updated, allowedUnitIDs), nil
}

func (r *PostgresItemRepository) Delete(ctx context.Context, id string) error {
	idUUID, err := uuidParam(id)
	if err != nil {
		return fmt.Errorf("invalid item id: %w", err)
	}
	_, err = queriesFor(ctx, r.db).DeleteItem(ctx, idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrItemNotFound
		}
		// Fires from either recipe_ingredients or shopping_list_items (both RESTRICT);
		// item_allowed_units is CASCADE and cleaned up automatically.
		if pgErrorCode(err, pgerrcode.RestrictViolation) {
			return models.ErrItemInUse
		}
		return fmt.Errorf("failed to delete item: %w", err)
	}
	return nil
}

// insertAllowedUnits parses, deduplicates, and inserts unitIDs as item_allowed_units rows for
// itemID, returning the deduplicated set actually inserted (order preserved, first-seen wins).
func (r *PostgresItemRepository) insertAllowedUnits(ctx context.Context, itemID pgtype.UUID, unitIDs []string) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]bool, len(unitIDs))
	result := make([]uuid.UUID, 0, len(unitIDs))
	for _, raw := range unitIDs {
		unitUUID, err := uuidParam(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid unit id: %w", err)
		}
		parsed := uuidValue(unitUUID)
		if seen[parsed] {
			continue
		}
		seen[parsed] = true

		if err := queriesFor(ctx, r.db).CreateItemAllowedUnit(ctx, sqlc.CreateItemAllowedUnitParams{
			ItemID: itemID,
			UnitID: unitUUID,
		}); err != nil {
			if constraintErr := pgConstraintError(err, itemAllowedUnitConstraintErrors); constraintErr != nil {
				return nil, constraintErr
			}
			return nil, fmt.Errorf("failed to add item allowed unit: %w", err)
		}
		result = append(result, parsed)
	}
	return result, nil
}

func toModelItem(item sqlc.Item, allowedUnitIDs []uuid.UUID) *models.Item {
	if allowedUnitIDs == nil {
		allowedUnitIDs = []uuid.UUID{}
	}
	return &models.Item{
		ID:             uuidValue(item.ID),
		Name:           item.Name,
		CategoryID:     uuidValue(item.CategoryID),
		AllowedUnitIDs: allowedUnitIDs,
		CreatedAt:      item.CreatedAt.Time,
		UpdatedAt:      item.UpdatedAt.Time,
	}
}

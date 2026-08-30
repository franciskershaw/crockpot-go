package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
)

var itemCategoryConstraintErrors = map[string]error{
	"item_categories_name_key": models.ErrItemCategoryNameTaken,
	"item_categories_icon_key": models.ErrItemCategoryIconTaken,
}

type PostgresItemCategoryRepository struct {
	db sqlc.DBTX
}

func NewPostgresItemCategoryRepository(db sqlc.DBTX) *PostgresItemCategoryRepository {
	return &PostgresItemCategoryRepository{db: db}
}

func (r *PostgresItemCategoryRepository) List(ctx context.Context) ([]*models.ItemCategory, error) {
	rows, err := queriesFor(ctx, r.db).ListItemCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list item categories: %w", err)
	}
	categories := make([]*models.ItemCategory, len(rows))
	for i, row := range rows {
		categories[i] = toModelItemCategory(row)
	}
	return categories, nil
}

func (r *PostgresItemCategoryRepository) Create(ctx context.Context, name, icon string) (*models.ItemCategory, error) {
	created, err := queriesFor(ctx, r.db).CreateItemCategory(ctx, sqlc.CreateItemCategoryParams{
		Name: name,
		Icon: icon,
	})
	if err != nil {
		if constraintErr := pgConstraintError(err, itemCategoryConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to create item category: %w", err)
	}
	return toModelItemCategory(created), nil
}

func (r *PostgresItemCategoryRepository) Update(ctx context.Context, id, name, icon string) (*models.ItemCategory, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid item category id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).UpdateItemCategory(ctx, sqlc.UpdateItemCategoryParams{
		Name: name,
		Icon: icon,
		ID:   idUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrItemCategoryNotFound
		}
		if constraintErr := pgConstraintError(err, itemCategoryConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to update item category: %w", err)
	}
	return toModelItemCategory(updated), nil
}

func (r *PostgresItemCategoryRepository) Delete(ctx context.Context, id string) error {
	idUUID, err := uuidParam(id)
	if err != nil {
		return fmt.Errorf("invalid item category id: %w", err)
	}
	_, err = queriesFor(ctx, r.db).DeleteItemCategory(ctx, idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrItemCategoryNotFound
		}
		// items.category_id is ON DELETE RESTRICT, so this is RestrictViolation, not ForeignKeyViolation.
		if pgErrorCode(err, pgerrcode.RestrictViolation) {
			return models.ErrItemCategoryInUse
		}
		return fmt.Errorf("failed to delete item category: %w", err)
	}
	return nil
}

func toModelItemCategory(c sqlc.ItemCategory) *models.ItemCategory {
	return &models.ItemCategory{
		ID:        uuidValue(c.ID),
		Name:      c.Name,
		Icon:      c.Icon,
		CreatedAt: c.CreatedAt.Time,
		UpdatedAt: c.UpdatedAt.Time,
	}
}

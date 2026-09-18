package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresShoppingListRepository struct {
	db sqlc.DBTX
}

func NewPostgresShoppingListRepository(db sqlc.DBTX) *PostgresShoppingListRepository {
	return &PostgresShoppingListRepository{db: db}
}

func (r *PostgresShoppingListRepository) Get(ctx context.Context, userID string) (*models.ShoppingList, error) {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	listID, err := q.GetShoppingListByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return &models.ShoppingList{Items: []models.ShoppingListItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get shopping list: %w", err)
	}

	rows, err := q.ListShoppingListItemsHydrated(ctx, listID)
	if err != nil {
		return nil, fmt.Errorf("failed to list shopping list items: %w", err)
	}

	items := make([]models.ShoppingListItem, len(rows))
	for i, row := range rows {
		qty, err := numericValue(row.Quantity)
		if err != nil {
			return nil, fmt.Errorf("failed to read shopping list item quantity: %w", err)
		}
		item := models.ShoppingListItem{
			ID:               uuidValue(row.ID),
			ItemID:           uuidValue(row.ItemID),
			ItemName:         row.ItemName,
			ItemCategoryID:   uuidValue(row.ItemCategoryID),
			ItemCategoryName: row.ItemCategoryName,
			Quantity:         qty,
			Obtained:         row.Obtained,
			IsManual:         row.IsManual,
		}
		if row.UnitID.Valid {
			u := uuidValue(row.UnitID)
			item.UnitID = &u
		}
		item.UnitAbbreviation = textPtr(row.UnitAbbreviation)
		items[i] = item
	}

	return &models.ShoppingList{Items: items}, nil
}

func (r *PostgresShoppingListRepository) Regenerate(ctx context.Context, userID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}

	// This upsert's row lock also serializes concurrent Regenerate calls for the same user —
	// the second blocks here until the first commits, so no separate lock is needed below.
	listID, err := q.GetOrCreateShoppingList(ctx, uid)
	if err != nil {
		return fmt.Errorf("failed to get or create shopping list: %w", err)
	}

	var aggregate []sqlc.AggregateMenuIngredientsRow
	menuID, err := q.GetMenuByUserID(ctx, uid)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No menu at all yet is equivalent to an empty one: fall through with a nil aggregate.
	case err != nil:
		return fmt.Errorf("failed to get menu: %w", err)
	default:
		aggregate, err = q.AggregateMenuIngredients(ctx, menuID)
		if err != nil {
			return fmt.Errorf("failed to aggregate menu ingredients: %w", err)
		}
	}

	itemIDs := make([]pgtype.UUID, len(aggregate))
	unitIDs := make([]pgtype.UUID, len(aggregate))
	quantities := make([]pgtype.Numeric, len(aggregate))
	for i, row := range aggregate {
		itemIDs[i] = row.ItemID
		unitIDs[i] = row.UnitID
		quantities[i] = row.Quantity
	}

	if err := q.SyncShoppingListItemQuantities(ctx, sqlc.SyncShoppingListItemQuantitiesParams{
		ShoppingListID: listID,
		ItemIds:        itemIDs,
		UnitIds:        unitIDs,
		Quantities:     quantities,
	}); err != nil {
		return fmt.Errorf("failed to sync shopping list item quantities: %w", err)
	}

	if err := q.InsertNewShoppingListItems(ctx, sqlc.InsertNewShoppingListItemsParams{
		ShoppingListID: listID,
		ItemIds:        itemIDs,
		UnitIds:        unitIDs,
		Quantities:     quantities,
	}); err != nil {
		return fmt.Errorf("failed to insert new shopping list items: %w", err)
	}

	if err := q.DeleteStaleDismissals(ctx, sqlc.DeleteStaleDismissalsParams{
		ShoppingListID: listID,
		ItemIds:        itemIDs,
		UnitIds:        unitIDs,
		Quantities:     quantities,
	}); err != nil {
		return fmt.Errorf("failed to delete stale dismissals: %w", err)
	}

	if err := q.DeleteObsoleteShoppingListItems(ctx, sqlc.DeleteObsoleteShoppingListItemsParams{
		ShoppingListID: listID,
		ItemIds:        itemIDs,
		UnitIds:        unitIDs,
	}); err != nil {
		return fmt.Errorf("failed to delete obsolete shopping list items: %w", err)
	}

	return nil
}

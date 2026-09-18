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

var shoppingListConstraintErrors = map[string]error{
	"shopping_list_items_item_id_fkey": models.ErrShoppingListInvalidItem,
	"shopping_list_items_unit_id_fkey": models.ErrShoppingListInvalidUnit,
}

type PostgresShoppingListRepository struct {
	db sqlc.DBTX
}

func NewPostgresShoppingListRepository(db sqlc.DBTX) *PostgresShoppingListRepository {
	return &PostgresShoppingListRepository{db: db}
}

// AddManualItem merges into an existing manual row for the same (itemID, unitID) by summing
// quantity; never merges into a generated row, since Regenerate would just overwrite it.
func (r *PostgresShoppingListRepository) AddManualItem(ctx context.Context, userID, itemID string, unitID *string, quantity float64) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	iid, err := uuid.Parse(itemID)
	if err != nil {
		return fmt.Errorf("invalid item id: %w", err)
	}
	var parsedUnitID *uuid.UUID
	if unitID != nil {
		parsed, err := uuid.Parse(*unitID)
		if err != nil {
			return fmt.Errorf("invalid unit id: %w", err)
		}
		parsedUnitID = &parsed
	}

	if err := checkAllowedUnits(ctx, q, []models.Ingredient{{ItemID: iid, UnitID: parsedUnitID}}); err != nil {
		return err
	}

	qty, err := numericParam(quantity)
	if err != nil {
		return fmt.Errorf("invalid quantity: %w", err)
	}

	listID, err := q.GetOrCreateShoppingList(ctx, uid)
	if err != nil {
		return fmt.Errorf("failed to get or create shopping list: %w", err)
	}

	pgItemID := pgUUID(iid)
	var pgUnitID pgtype.UUID
	if parsedUnitID != nil {
		pgUnitID = pgUUID(*parsedUnitID)
	}

	existing, err := q.FindManualShoppingListItem(ctx, sqlc.FindManualShoppingListItemParams{
		ShoppingListID: listID,
		ItemID:         pgItemID,
		UnitID:         pgUnitID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := q.InsertManualShoppingListItem(ctx, sqlc.InsertManualShoppingListItemParams{
			ShoppingListID: listID,
			ItemID:         pgItemID,
			UnitID:         pgUnitID,
			Quantity:       qty,
		}); err != nil {
			if mapped := pgConstraintError(err, shoppingListConstraintErrors); mapped != nil {
				return mapped
			}
			return fmt.Errorf("failed to insert manual shopping list item: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to find existing manual shopping list item: %w", err)
	default:
		if err := q.IncrementShoppingListItemQuantity(ctx, sqlc.IncrementShoppingListItemQuantityParams{
			Delta: qty,
			ID:    existing.ID,
		}); err != nil {
			return fmt.Errorf("failed to increment manual shopping list item: %w", err)
		}
		return nil
	}
}

func (r *PostgresShoppingListRepository) UpdateItem(ctx context.Context, userID, itemRowID string, obtained *bool, quantity *float64) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(itemRowID)
	if err != nil {
		return fmt.Errorf("invalid item row id: %w", err)
	}

	listID, err := q.GetShoppingListByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrShoppingListItemNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get shopping list: %w", err)
	}

	var qtyParam pgtype.Numeric
	if quantity != nil {
		qtyParam, err = numericParam(*quantity)
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
	}

	affected, err := q.UpdateShoppingListItem(ctx, sqlc.UpdateShoppingListItemParams{
		Obtained:       pgtype.Bool{Bool: obtained != nil && *obtained, Valid: obtained != nil},
		Quantity:       qtyParam,
		ID:             rid,
		ShoppingListID: listID,
	})
	if err != nil {
		return fmt.Errorf("failed to update shopping list item: %w", err)
	}
	if affected == 0 {
		return models.ErrShoppingListItemNotFound
	}
	return nil
}

func (r *PostgresShoppingListRepository) DeleteItem(ctx context.Context, userID, itemRowID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(itemRowID)
	if err != nil {
		return fmt.Errorf("invalid item row id: %w", err)
	}

	listID, err := q.GetShoppingListByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrShoppingListItemNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get shopping list: %w", err)
	}

	deleted, err := q.DeleteShoppingListItem(ctx, sqlc.DeleteShoppingListItemParams{
		ID:             rid,
		ShoppingListID: listID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrShoppingListItemNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to delete shopping list item: %w", err)
	}

	if deleted.IsManual {
		return nil
	}

	if err := q.InsertDismissedItem(ctx, sqlc.InsertDismissedItemParams{
		ShoppingListID:      listID,
		ItemID:              deleted.ItemID,
		UnitID:              deleted.UnitID,
		QuantityAtDismissal: deleted.Quantity,
	}); err != nil {
		return fmt.Errorf("failed to insert dismissed item: %w", err)
	}
	return nil
}

func (r *PostgresShoppingListRepository) ClearList(ctx context.Context, userID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}

	if err := q.ClearShoppingListItems(ctx, uid); err != nil {
		return fmt.Errorf("failed to clear shopping list: %w", err)
	}
	return nil
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

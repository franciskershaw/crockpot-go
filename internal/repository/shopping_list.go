package repository

import (
	"context"
	"errors"
	"fmt"

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

func (r *PostgresShoppingListRepository) Regenerate(ctx context.Context, userID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}

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

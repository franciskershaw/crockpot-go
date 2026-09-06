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

type PostgresMenuRepository struct {
	db sqlc.DBTX
}

func NewPostgresMenuRepository(db sqlc.DBTX) *PostgresMenuRepository {
	return &PostgresMenuRepository{db: db}
}

func (r *PostgresMenuRepository) GetMenu(ctx context.Context, userID string) (*models.Menu, error) {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	menuID, err := q.GetMenuByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return &models.Menu{Entries: []models.MenuEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get menu: %w", err)
	}

	rows, err := q.ListMenuEntries(ctx, menuID)
	if err != nil {
		return nil, fmt.Errorf("failed to list menu entries: %w", err)
	}

	entries := make([]models.MenuEntry, len(rows))
	cards := make([]*models.RecipeCard, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		card := &models.RecipeCard{
			ID:            uuidValue(row.ID),
			Name:          row.Name,
			ImageURL:      textPtr(row.ImageUrl),
			ImageFilename: textPtr(row.ImageFilename),
			TimeInMinutes: int(row.TimeInMinutes),
			Serves:        int(row.Serves),
			Approved:      row.Approved,
			Categories:    []models.CategoryRef{},
			CreatedAt:     row.CreatedAt.Time,
		}
		cards[i] = card
		ids[i] = row.ID
		entries[i] = models.MenuEntry{
			RecipeID: uuidValue(row.ID),
			Serves:   int(row.MenuServes),
			Recipe:   card,
		}
	}

	if err := hydrateCardCategories(ctx, q, cards, ids); err != nil {
		return nil, err
	}

	return &models.Menu{Entries: entries}, nil
}

func (r *PostgresMenuRepository) UpsertEntry(ctx context.Context, userID, recipeID string, serves int, callerIsAdmin bool) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(recipeID)
	if err != nil {
		return fmt.Errorf("invalid recipe id: %w", err)
	}

	visible, err := q.RecipeVisibleToCaller(ctx, sqlc.RecipeVisibleToCallerParams{
		ID:            rid,
		CallerIsAdmin: callerIsAdmin,
		CallerID:      uid,
	})
	if err != nil {
		return fmt.Errorf("failed to check recipe visibility: %w", err)
	}
	if !visible {
		return models.ErrRecipeNotFound
	}

	menuID, err := q.GetOrCreateMenu(ctx, uid)
	if err != nil {
		return fmt.Errorf("failed to get or create menu: %w", err)
	}

	if err := q.UpsertMenuEntry(ctx, sqlc.UpsertMenuEntryParams{
		RecipeMenuID: menuID,
		RecipeID:     rid,
		Serves:       int32(serves),
	}); err != nil {
		return fmt.Errorf("failed to upsert menu entry: %w", err)
	}
	return nil
}

func (r *PostgresMenuRepository) UpdateEntryServes(ctx context.Context, userID, recipeID string, serves int) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(recipeID)
	if err != nil {
		return fmt.Errorf("invalid recipe id: %w", err)
	}

	menuID, err := q.GetMenuByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrMenuEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get menu: %w", err)
	}

	affected, err := q.UpdateMenuEntryServes(ctx, sqlc.UpdateMenuEntryServesParams{
		RecipeMenuID: menuID,
		RecipeID:     rid,
		Serves:       int32(serves),
	})
	if err != nil {
		return fmt.Errorf("failed to update menu entry: %w", err)
	}
	if affected == 0 {
		return models.ErrMenuEntryNotFound
	}
	return nil
}

func (r *PostgresMenuRepository) RemoveEntry(ctx context.Context, userID, recipeID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(recipeID)
	if err != nil {
		return fmt.Errorf("invalid recipe id: %w", err)
	}

	menuID, err := q.GetMenuByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get menu: %w", err)
	}

	if err := q.RemoveMenuEntry(ctx, sqlc.RemoveMenuEntryParams{
		RecipeMenuID: menuID,
		RecipeID:     rid,
	}); err != nil {
		return fmt.Errorf("failed to remove menu entry: %w", err)
	}
	return nil
}

package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type seededMaps struct {
	itemCategories   map[string]uuid.UUID
	units            map[string]uuid.UUID
	recipeCategories map[string]uuid.UUID
}

func openPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return pgxpool.NewWithConfig(ctx, cfg)
}

func fetchSeeded(ctx context.Context, pool *pgxpool.Pool) (*seededMaps, error) {
	q := sqlc.New(pool)
	ics, err := q.ListItemCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list item categories: %w", err)
	}
	units, err := q.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	rcs, err := q.ListRecipeCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recipe categories: %w", err)
	}
	if len(ics) == 0 || len(units) == 0 || len(rcs) == 0 {
		return nil, fmt.Errorf(
			"reference tables are not seeded (item_categories=%d units=%d recipe_categories=%d); run migrations first",
			len(ics), len(units), len(rcs))
	}
	m := &seededMaps{
		itemCategories:   make(map[string]uuid.UUID, len(ics)),
		units:            make(map[string]uuid.UUID, len(units)),
		recipeCategories: make(map[string]uuid.UUID, len(rcs)),
	}
	for _, c := range ics {
		m.itemCategories[c.Name] = uuid.UUID(c.ID.Bytes)
	}
	for _, u := range units {
		m.units[u.Name] = uuid.UUID(u.ID.Bytes)
	}
	for _, c := range rcs {
		m.recipeCategories[c.Name] = uuid.UUID(c.ID.Bytes)
	}
	return m, nil
}

func buildResolvers(src *source, m *seededMaps) (ic, un, rc *refResolver, misses []refMiss) {
	icRows := make([]refRow, len(src.ItemCategories))
	for i, c := range src.ItemCategories {
		icRows[i] = refRow(c)
	}
	unRows := make([]refRow, len(src.Units))
	for i, u := range src.Units {
		unRows[i] = refRow{ID: u.ID, Name: u.Name}
	}
	rcRows := make([]refRow, len(src.RecipeCategories))
	for i, c := range src.RecipeCategories {
		rcRows[i] = refRow(c)
	}

	ic, m1 := buildRefResolver("item category", icRows, m.itemCategories)
	un, m2 := buildRefResolver("unit", unRows, m.units)
	rc, m3 := buildRefResolver("recipe category", rcRows, m.recipeCategories)
	misses = append(misses, m1...)
	misses = append(misses, m2...)
	misses = append(misses, m3...)
	return ic, un, rc, misses
}

func load(ctx context.Context, pool *pgxpool.Pool, res *transformResult) (map[string]int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	if err := q.MigrateTruncate(ctx); err != nil {
		return nil, fmt.Errorf("truncate: %w", err)
	}
	if err := deleteMigratedUsers(ctx, q, res.Users); err != nil {
		return nil, err
	}
	if err := insertUsers(ctx, q, res.Users); err != nil {
		return nil, err
	}
	if err := insertItems(ctx, q, res.Items); err != nil {
		return nil, err
	}
	if err := insertRecipes(ctx, q, res.Recipes); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return countPersisted(ctx, pool)
}

func countPersisted(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	queries := map[string]string{
		"users":                     "SELECT count(*) FROM users",
		"items":                     "SELECT count(*) FROM items",
		"item_allowed_units":        "SELECT count(*) FROM item_allowed_units",
		"recipes":                   "SELECT count(*) FROM recipes",
		"recipe_ingredients":        "SELECT count(*) FROM recipe_ingredients",
		"recipe_categories_recipes": "SELECT count(*) FROM recipe_categories_recipes",
	}
	out := make(map[string]int, len(queries))
	for name, q := range queries {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", name, err)
		}
		out[name] = n
	}
	return out, nil
}

func deleteMigratedUsers(ctx context.Context, q *sqlc.Queries, users []userRow) error {
	ids := make([]pgtype.UUID, len(users))
	gids := make([]string, len(users))
	for i, u := range users {
		ids[i] = pgUUID(u.ID)
		gids[i] = u.GoogleID
	}
	if err := q.MigrateDeleteUsers(ctx, sqlc.MigrateDeleteUsersParams{Ids: ids, GoogleIds: gids}); err != nil {
		return fmt.Errorf("delete existing migrated users: %w", err)
	}
	return nil
}

func insertUsers(ctx context.Context, q *sqlc.Queries, users []userRow) error {
	for _, u := range users {
		err := q.MigrateInsertUser(ctx, sqlc.MigrateInsertUserParams{
			ID:              pgUUID(u.ID),
			Email:           u.Email,
			GoogleID:        pgText(u.GoogleID),
			Name:            pgTextPtr(u.Name),
			Image:           pgTextPtr(u.Image),
			Role:            u.Role,
			EmailVerifiedAt: pgTimestamptzPtr(u.EmailVerifiedAt),
			CreatedAt:       pgTimestamptz(u.CreatedAt),
			UpdatedAt:       pgTimestamptz(u.UpdatedAt),
		})
		if err != nil {
			return fmt.Errorf("insert user %s: %w", u.Email, err)
		}
	}
	return nil
}

func insertItems(ctx context.Context, q *sqlc.Queries, items []itemRow) error {
	for _, it := range items {
		err := q.MigrateInsertItem(ctx, sqlc.MigrateInsertItemParams{
			ID:         pgUUID(it.ID),
			Name:       it.Name,
			CategoryID: pgUUID(it.CategoryID),
			CreatedAt:  pgTimestamptz(it.CreatedAt),
			UpdatedAt:  pgTimestamptz(it.UpdatedAt),
		})
		if err != nil {
			return fmt.Errorf("insert item %s: %w", it.Name, err)
		}
		for _, unitID := range it.AllowedUnitIDs {
			if err := q.MigrateInsertItemAllowedUnit(ctx, sqlc.MigrateInsertItemAllowedUnitParams{
				ItemID: pgUUID(it.ID),
				UnitID: pgUUID(unitID),
			}); err != nil {
				return fmt.Errorf("insert allowed unit for item %s: %w", it.Name, err)
			}
		}
	}
	return nil
}

func insertRecipes(ctx context.Context, q *sqlc.Queries, recipes []recipeRow) error {
	for _, r := range recipes {
		err := q.MigrateInsertRecipe(ctx, sqlc.MigrateInsertRecipeParams{
			ID:            pgUUID(r.ID),
			Name:          r.Name,
			TimeInMinutes: int32(r.TimeInMinutes),
			ImageUrl:      pgTextPtr(r.ImageURL),
			ImageFilename: pgTextPtr(r.ImageFilename),
			Instructions:  r.Instructions,
			Notes:         r.Notes,
			Approved:      r.Approved,
			Serves:        int32(r.Serves),
			CreatedByID:   pgUUIDPtr(r.CreatedByID),
			CreatedByName: pgTextPtr(r.CreatedByName),
			CreatedAt:     pgTimestamptz(r.CreatedAt),
			UpdatedAt:     pgTimestamptz(r.UpdatedAt),
		})
		if err != nil {
			return fmt.Errorf("insert recipe %s: %w", r.Name, err)
		}
		for _, ing := range r.Ingredients {
			qty, err := toNumeric(ing.Quantity)
			if err != nil {
				return fmt.Errorf("recipe %s quantity %v: %w", r.Name, ing.Quantity, err)
			}
			if err := q.MigrateInsertRecipeIngredient(ctx, sqlc.MigrateInsertRecipeIngredientParams{
				RecipeID: pgUUID(r.ID),
				ItemID:   pgUUID(ing.ItemID),
				UnitID:   pgUUIDPtr(ing.UnitID),
				Quantity: qty,
				Position: int16(ing.Position),
			}); err != nil {
				return fmt.Errorf("insert ingredient for recipe %s: %w", r.Name, err)
			}
		}
		for _, catID := range r.CategoryIDs {
			if err := q.MigrateInsertRecipeCategoryLink(ctx, sqlc.MigrateInsertRecipeCategoryLinkParams{
				RecipeID:   pgUUID(r.ID),
				CategoryID: pgUUID(catID),
			}); err != nil {
				return fmt.Errorf("insert category link for recipe %s: %w", r.Name, err)
			}
		}
	}
	return nil
}

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func pgUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*id)
}

func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func pgTextPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func pgTimestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func pgTimestamptzPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func toNumeric(f float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(f, 'f', -1, 64)); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}

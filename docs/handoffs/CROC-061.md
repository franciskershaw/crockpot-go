# CROC-061 — Non-ingredient item categories

**Implementation mode: AI-driven.**

## Summary

Item categories gain `is_ingredient` (false for House: toilet paper,
bin bags, dishwasher tablets…), returned by `GET /item-categories` as
`isIngredient`. Recipe create/update reject an ingredient whose item is
in a non-ingredient category. `crockpot-react` filters its one cached
`GET /items` list by this flag in the recipe form (`CFE-010` 10b) and
browse's ingredient filter (`CFE-047`); the shopping list keeps every
item. Grilled 2026-09-30. Mostly cheap to undo; the field name and error
code are the contract the frontend reads.

## Facts checked before deciding

- House is seeded as `('House', 'House')`
  (`db/migrations/000003_seed_item_categories.up.sql:7`); next migration
  is `000013`. Migrations apply on startup (`db/db.go:72`).
- `ListItemCategories` is `SELECT *` (`internal/sqlc/queries/item_categories.sql:1`),
  so the column reaches `models.ItemCategory` via sqlc regenerate.
- `checkAllowedUnits` (`internal/repository/recipe.go:503`) runs in the
  transaction for recipe create (`:325`) and update (`:381`), and is
  **also** used by shopping-list manual add (`shopping_list.go:50`) —
  House items must stay addable there.
- `writeRecipeWriteError` (`internal/handler/recipe_handler.go:297`) maps
  repository errors to 400 codes (`unit_not_allowed_for_item`, …).
- `cmd/migrate-data` only reads `item_categories` (`load.go:33`, resolves
  by name) and never truncates it, so a re-import can't reset the flag.
- No migrated recipe uses a House item (0 of 213 in `crockpotV3.Recipe.json`).

## Decisions

1. **`is_ingredient BOOLEAN NOT NULL DEFAULT true`**, JSON `isIngredient`,
   set `false` for House by migration. Positive naming so callers read
   the flag directly rather than negate a `household` one, and isn't tied
   to "household" if another non-ingredient category appears; default
   true so a forgotten flag leaves items searchable rather than silently
   hidden.
2. **Migration-only** — not settable through `POST`/`PATCH
   /item-categories`. No admin category UI exists; a new non-ingredient
   category is rare and gets its own migration; avoids an API flip
   invalidating recipes that already use that category. Revisit if an
   admin category UI is built.
3. **400 `item_not_ingredient`** from a new `checkIngredientItems` in
   `repository/recipe.go`, in the same transaction as `checkAllowedUnits`
   on create and update only, returning a new
   `models.ErrRecipeNonIngredientItem`. A separate function so the
   shopping list's shared `checkAllowedUnits` path is untouched. The
   frontend filters these items out, so the code is a backstop, shown as
   the form's generic error.
4. `GET /items` is unchanged; the frontend joins items to categories.

## Non-goals

- Admin-editable flag (decision 2).
- Any shopping-list or menu behaviour change.
- Filtering `GET /items` server-side.

## Acceptance criteria

- [ ] Migration `000013` adds the column (default true, not null), sets
      House false; down migration drops it.
- [ ] `GET /item-categories` returns `isIngredient` on every category:
      false for House, true for the rest.
- [ ] `POST /recipes` and `PATCH /recipes/:id` with a House item return
      400 `item_not_ingredient`; nothing is written.
- [ ] Recipe create/update with only ingredient items is unaffected.
- [ ] Shopping-list manual add of a House item still succeeds.
- [ ] `requests/item-categories.http` shows `isIngredient`;
      `requests/recipes.http` covers the House-item rejection.

## Pieces and verification

Each piece: failing tests first, stop at red, then green, then stop.

1. **Column and read** — migration, sqlc regenerate, model field.
   Repository test (Neon): `ListItemCategories` returns House with
   `IsIngredient` false and the rest true. Handler test: the list JSON
   carries `isIngredient`. (logic + API boundary)
2. **Recipe rejection** — `checkIngredientItems`, the error, the handler
   mapping. Repository tests (Neon): create and update with a House item
   return `ErrRecipeNonIngredientItem` and write nothing; shopping-list
   manual add of a House item still succeeds. Handler test: maps to 400
   `item_not_ingredient`. (logic)
3. **`.http` walkthrough** — the two files above, run top to bottom
   against a local server. (API boundary)

Commands: `go test ./internal/handler/...`, `./scripts/test-repo.sh`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`,
then `branch-review` before close-out.

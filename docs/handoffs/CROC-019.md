# CROC-019 — Menu read/upsert-entry (GET /menu, POST /menu/entries, PATCH/DELETE /menu/entries/:recipeId)

**Implementation mode: AI-driven.** Claude works one piece at a time (see
"Piece order" below): failing test(s) first, a stub that fails for the
right reason, confirm red, stop for review, implement on go-ahead,
confirm green, stop again before the next piece.

## Summary

First write path for a user's weekly menu. `recipe_menus`/
`recipe_menu_entries`/`menu_history_entries` already exist
(`db/migrations/000001_init.up.sql:150-179`, from `CROC-001`) but hold no
data and nothing writes to them yet. This ticket builds `GET /menu`,
`POST /menu/entries` (upsert), `PATCH /menu/entries/:recipeId` (change
serves), `DELETE /menu/entries/:recipeId` — everything except
`menu_history_entries` writes (`CROC-020`) and shopping-list regeneration
(`CROC-021`), both deliberately separate tickets. Unblocks
`crockpot-react`'s `CFE-020` (browse-card add-to-menu) and `CFE-005`'s
already-listed detail-page add-to-menu action — first of this session's
browse-page-completeness phase (see `master-spec.md`'s 2026-09-06 note).

## Decisions from the interview

### 1. Atomic upsert via a new unique constraint, not read-then-write

`recipe_menu_entries` today has no constraint stopping two rows for the
same `(recipe_menu_id, recipe_id)`. The old app's
`addRecipeToMenu` (`crockpot/src/data/menu/menuMutations.ts:58-113`) did
read-then-write against a Mongo embedded array — the only option that
storage model allowed. This codebase already has a strictly better
precedent for "add to my collection, upsert if present" on Postgres:
`AddFavourite`'s `INSERT ... ON CONFLICT (user_id, recipe_id) DO NOTHING`
(`internal/sqlc/queries/favourites.sql:1-3`). Reusing that shape here (as
`DO UPDATE` instead of `DO NOTHING`, since re-adding should update
`serves`) closes a TOCTOU gap the old app's storage model couldn't avoid
— the same failure class this project has already been burned by once
(`CROC-008`'s reuse-detection race, `LESSONS.md` 2026-08-17).

Migration `000010_recipe_menu_entries_upsert_support`:

```sql
-- up
ALTER TABLE recipe_menu_entries
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE recipe_menu_entries
    ADD CONSTRAINT recipe_menu_entries_menu_recipe_key UNIQUE (recipe_menu_id, recipe_id);

-- down
ALTER TABLE recipe_menu_entries
    DROP CONSTRAINT recipe_menu_entries_menu_recipe_key;
ALTER TABLE recipe_menu_entries
    DROP COLUMN created_at;
```

(Bundled with decision 5's `created_at` column — both touch the same
table, no reason to split into two migrations.)

### 2. Menu row created lazily on first `POST`; `GET /menu` never 404s and never writes

Matches the old app's own lazy-creation precedent
(`menuMutations.ts:95-107`, `prisma.recipeMenu.upsert`) — no
`recipe_menus` row exists until the first entry is added.
`GET /menu` for a user with no row yet returns `200 {"entries": []}`,
synthesized, not read from a row that doesn't exist — a full-auth page
load (Your Crockpot) shouldn't have to special-case a 404 into "empty
menu," and a `GET` must not have the side effect of creating a row (that
stays `POST`'s job, keeping write-amplification off read-heavy page-load
traffic). Get-or-create the menu row via the same
`ON CONFLICT ... DO UPDATE ... RETURNING id` trick (a true `DO NOTHING`
skips `RETURNING` on the conflicting path):

```sql
-- name: GetOrCreateMenu :one
INSERT INTO recipe_menus (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = excluded.user_id
RETURNING id;
```

### 3. Full auth on all four routes; recipe-visibility check only on add

Mirrors `CROC-018` favourites exactly: `AuthMiddleware` (no anonymous
case — a menu has nothing to show without an owner), and `POST` reuses
the existing `RecipeVisibleToCaller` check `AddFavourite` already runs
(`favourite.go:24-34`) before inserting — a recipe you can't see can't be
added to your menu either, `404 {"error":"recipe_not_found"}` on failure.
`PATCH`/`DELETE` do **not** re-check visibility, matching
`RemoveFavourite`'s precedent (`favourite.go:45-64`) — a recipe already
on your menu stays manageable there regardless of a later approval-state
change.

### 4. `serves` validated 1-50 (matches `CROC-014`'s recipe `serves` field exactly); `DELETE` idempotent, `PATCH` strict

`parseCreateRecipeInput` already validates a recipe's own `serves` as
`req.Serves < 1 || req.Serves > 50` → `badRequest(c, "invalid_serves")`
(`recipe_requests.go:50-53`) — a menu entry's `serves` gets the identical
bound and error code, not a fresh range invented for this ticket.

- `POST /menu/entries` (`{recipeId, serves}` in body): out-of-range
  `serves` or malformed `recipeId` → `400`. Upserts per decision 1.
- `PATCH /menu/entries/:recipeId` (`{serves}` in body): out-of-range →
  `400`. Recipe not currently on the menu → `404
  {"error":"menu_entry_not_found"}` — `PATCH` means "change an existing
  entry's serves," there's no sensible value to invent for an entry that
  doesn't exist, and silently upserting here would blur `PATCH` with
  `POST`'s already-distinct job.
- `DELETE /menu/entries/:recipeId`: idempotent — removing a recipe not on
  the menu is `200`, not `404` (matches `RemoveFavourite`'s idempotent
  toggle; "not there" and "successfully removed" are the same end state,
  unlike `PATCH`).

### 5. `created_at` added to `recipe_menu_entries`; default order `created_at DESC`; untouched by the upsert's `DO UPDATE`

Repeats an already-made decision in this exact codebase: `recipe_favourites`
originally had no `created_at` either, and got one added post-hoc,
specifically to support ordering by "most recently added"
(`000009_add_recipe_favourites_created_at`, `LESSONS.md` 2026-09-02 note
on `CROC-018`). Same pattern here — caught before shipping instead of
after. `GET /menu`'s entries default-order `created_at DESC` (most
recently added first) — no `?sort=` param in this ticket (menus aren't
paginated, no stated need for an alternate order yet), but the column's
existence leaves that door open for a future ticket without a further
schema change. The upsert's `DO UPDATE SET serves = excluded.serves`
touches only `serves` — bumping an existing entry's serving size does not
reset its `created_at`, so it doesn't reshuffle position.

### 6. Write responses are `{"message": "..."}`, not the hydrated entry

Matches `CROC-018`'s favourites decision 4 exactly, same reasoning: the
frontend already holds the recipe card when the user clicks "add to
menu" (optimistic client-side update, same shape as the favourite heart)
and has no use for a hydrated body on the write path. Only `GET /menu`
returns hydrated data. `POST`/`PATCH`/`DELETE` all `200
{"message": "..."}`.

## Data layer shape

New resource, own handler/repository (`menu_handler.go`,
`internal/repository/menu.go`, `internal/sqlc/queries/menu.sql`) —
doesn't belong on `RecipeHandler`/`RecipeRepository`, which own `/recipes`
specifically; `/menu` is its own top-level resource the way
`/item-categories`/`/units` are, not a recipe sub-route.

```go
type MenuRepository interface {
    GetMenu(ctx context.Context, userID string) (*models.Menu, error)
    UpsertEntry(ctx context.Context, userID, recipeID string, serves int, callerIsAdmin bool) error // models.ErrRecipeNotFound if hidden/nonexistent
    UpdateEntryServes(ctx context.Context, userID, recipeID string, serves int) error                // models.ErrMenuEntryNotFound if absent
    RemoveEntry(ctx context.Context, userID, recipeID string) error                                  // idempotent, never an error for "not present"
}
```

```sql
-- name: GetOrCreateMenu :one
INSERT INTO recipe_menus (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = excluded.user_id
RETURNING id;

-- name: GetMenuByUserID :one
SELECT id FROM recipe_menus WHERE user_id = $1;

-- name: UpsertMenuEntry :exec
INSERT INTO recipe_menu_entries (recipe_menu_id, recipe_id, serves)
VALUES ($1, $2, $3)
ON CONFLICT (recipe_menu_id, recipe_id) DO UPDATE SET serves = excluded.serves;

-- name: UpdateMenuEntryServes :execrows
UPDATE recipe_menu_entries
SET serves = $3
WHERE recipe_menu_id = $1 AND recipe_id = $2;

-- name: RemoveMenuEntry :exec
DELETE FROM recipe_menu_entries WHERE recipe_menu_id = $1 AND recipe_id = $2;

-- name: ListMenuEntries :many
SELECT rme.recipe_id, rme.serves, r.*
FROM recipe_menu_entries rme
JOIN recipes r ON r.id = rme.recipe_id
WHERE rme.recipe_menu_id = $1
ORDER BY rme.created_at DESC;
```

`UpdateMenuEntryServes` uses `:execrows` (sqlc's affected-row-count form,
already available via `pgconn.CommandTag`) so the repository can turn
`0` rows into `models.ErrMenuEntryNotFound` without a separate existence
query.

`GetMenu`: `GetMenuByUserID` → `pgx.ErrNoRows` → return
`&models.Menu{Entries: []models.MenuEntry{}}` (decision 2), no error.
Otherwise `ListMenuEntries` + reuse the existing `hydrateCardCategories`/
`toRecipeCard` helpers (`favourite.go`'s `ListFavourites` pattern) to
build each entry's `Recipe *models.RecipeCard`.

`UpsertEntry`: `RecipeVisibleToCaller` (decision 3) → `GetOrCreateMenu` →
`UpsertMenuEntry`. Not a transaction — same reasoning as `AddFavourite`
(no invariant a race would break beyond the dormant "recipe flips
approved→hidden mid-request" edge case, which nothing in this app can
currently trigger).

`models/menu.go`:

```go
type MenuEntry struct {
    RecipeID uuid.UUID  `json:"recipeId"`
    Serves   int        `json:"serves"`
    Recipe   *RecipeCard `json:"recipe"`
}

type Menu struct {
    Entries []MenuEntry `json:"entries"`
}
```

`models/errors.go` gains:

```go
var ErrMenuEntryNotFound = errors.New("menu entry not found")
```

Request bodies (`menu_requests.go`, mirroring `recipe_requests.go`'s
split-out-parsing convention):

```go
type upsertMenuEntryRequest struct {
    RecipeID string `json:"recipeId"`
    Serves   int    `json:"serves"`
}

type updateMenuEntryServesRequest struct {
    Serves int `json:"serves"`
}
```

Validation reuses existing helpers verbatim: `bindJSON`, `parseID` (for
`RecipeID`), and the inline `< 1 || > 50` → `invalid_serves` check
ported from `parseCreateRecipeInput`.

Routes (`main.go`, new group, same shape as `item-categories`/`units`):

```go
menu := server.Group("/menu")
menu.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess))
{
    menu.GET("", menuHandler.Get)
    menu.POST("/entries", menuHandler.UpsertEntry)
    menu.PATCH("/entries/:recipeId", menuHandler.UpdateEntryServes)
    menu.DELETE("/entries/:recipeId", menuHandler.RemoveEntry)
}
```

## Acceptance criteria

- [ ] Migration `000010` applies and reverts cleanly (`migrate up` /
      `migrate down 1` / `migrate up` round-trip against the real dev DB).
- [ ] `GET /menu`: `401` anonymous; authenticated with no menu row yet →
      `200 {"entries":[]}`; with entries → hydrated `RecipeCard` per
      entry, ordered `created_at DESC` (most recently added first).
- [ ] `POST /menu/entries`: `401` anonymous; `400` malformed `recipeId` /
      out-of-range `serves`; `404` hidden/nonexistent recipe; `200` +
      message on first add; `200` + message again with a different
      `serves` on a repeat call, and `GET /menu` reflects the updated
      `serves`, not a duplicate entry.
- [ ] `PATCH /menu/entries/:recipeId`: `401` anonymous; `400` malformed id
      / out-of-range `serves`; `404` when the recipe isn't on the
      caller's menu; `200` + message + `GET /menu` reflects the new
      `serves` when it is.
- [ ] `DELETE /menu/entries/:recipeId`: `401` anonymous; `200` + message
      whether or not the recipe was on the menu (idempotent); `GET /menu`
      no longer lists it after removal.
- [ ] Two concurrent `POST /menu/entries` for the same `(user, recipe)`
      never produce two rows — proven against the real dev DB (the
      unique constraint + `ON CONFLICT`), not just asserted by reading
      the SQL.
- [ ] `requests/menu.http` — new file, chained off `auth.http`'s `Login`
      per this project's convention, covering every case above end-to-end
      against a live server.

## Non-goals

- No `menu_history_entries` writes — `CROC-020`'s job. "You've made this
  before"-style data simply doesn't populate yet; not a bug.
- No shopping-list regeneration on add/remove — `CROC-021`, Epic 6, not
  built. The old app coupled this in (`menuMutations.ts:110,150,182`);
  this ticket deliberately doesn't, since that endpoint doesn't exist.
- No tier gating — weekly menu is FREE per the Tiers section of
  `master-spec.md`.
- No pagination on `GET /menu` — a personal list, expected small.
- No `?sort=` param — see decision 5.

## Verification modes

- **Migration**: `migrate up` / `migrate down 1` / `migrate up`
  round-trip against the real dev DB — same check as `CROC-032`/`CROC-009`
  (favourites' `000009`).
- **Repository layer** (service/API boundary): real requests against the
  real Neon dev DB, per method, via `./scripts/test-repo.sh`. Cover:
  lazy menu creation, the atomic upsert under concurrent calls, visibility
  gating on add, `PATCH`/`DELETE` on a non-existent entry, empty-menu
  `GetMenu`, ordering.
- **Handler layer** (logic with assertable behaviour): failing test
  first, `testify/mock`-backed `MenuRepository` (via `go tool mockery`),
  `go test ./internal/handler/...`. Cover: 401 on all four routes, 400
  validation cases, 404 mappings, the message-body shape.
- **`.http` suite**: `requests/menu.http`, run top-to-bottom against a
  live local server by the founder — the real end-to-end
  add→appears→update→disappears loop, which the mocked handler tests and
  per-method repo tests each only see half of.

## Piece order (AI-driven)

1. **Migration** `000010_recipe_menu_entries_upsert_support` (decisions
   1, 5). Round-trip verified against the real dev DB before moving on.
2. **`internal/sqlc/queries/menu.sql`** (Data layer shape above) +
   `sqlc generate` + `models/menu.go` + `models/errors.go`'s
   `ErrMenuEntryNotFound`.
3. **`internal/repository/menu.go`** — `GetMenu`, `UpsertEntry`,
   `UpdateEntryServes`, `RemoveEntry`. Failing tests first in
   `internal/repository/menu_test.go` (real dev DB), confirm red, then
   implement. Confirm green via `./scripts/test-repo.sh`.
4. **`internal/handler/menu_requests.go`** (request structs + parsing,
   mirroring `recipe_requests.go`) + **`internal/handler/menu_handler.go`**
   (`Get`/`UpsertEntry`/`UpdateEntryServes`/`RemoveEntry`). Failing tests
   first in `menu_handler_test.go` (mocked `MenuRepository`,
   `go tool mockery` regenerate), confirm red, then implement. Confirm
   green via `go test ./internal/handler/...`.
5. **`main.go`** — new `/menu` route group (Data layer shape above) +
   `requests/menu.http` (new file). Manual `.http` run against
   `go run .` is the founder's real end-to-end check.
6. Lint / format / `go mod tidy -diff` clean → `/code-review medium main`
   → close-out.

Grilled 2026-09-06.

Completed 2026-09-06.

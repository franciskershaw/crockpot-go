# CROC-018 — Favourites (POST/DELETE /recipes/:id/favourite, GET /recipes/favourites)

**Implementation mode: Hand-written.** The founder implements against this
doc; still test-first per each piece's verification mode below — hand-written
decides *who* writes the code, not test order. Once done, Claude runs
`/code-review medium main` and confirms the `.http` suite was actually run
against a live server before close-out.

## Summary

Adds favouriting: `POST`/`DELETE /recipes/:id/favourite` (toggle, both
idempotent), `GET /recipes/favourites` (the caller's own favourites,
paginated), and an additive `isFavourite` boolean on `CROC-015`'s
`RecipeCard`/`RecipeDetail` DTOs. The join table (`recipe_favourites`)
already exists from the `CROC-001` scaffold and holds no data — `CROC-024`
explicitly deferred migrating the old app's favourites
(`docs/handoffs/CROC-024.md:31,177,368`) — so this ships on a clean slate,
no backfill. Unblocks `crockpot-react`'s `CFE-004` (browse card heart),
`CFE-005` (detail page heart), `CFE-007` (favourites tab).

## Decisions from the interview

### 1. `recipe_favourites` gets a `created_at` column (schema change)

The table today is bare `(user_id, recipe_id)` — no way to order a
favourites list by "most recently favourited," only by the underlying
recipe's own `created_at` (which answers a different question: which
recipes are newest, not what you just did). `menu_history_entries`
already established the precedent of putting a timestamp on a junction
table specifically so it can be ordered by when the link was made
(`first_added_to_menu`, `db/migrations/000001_init.up.sql:169-175`) —
this is that same pattern, not a new one. `recipe_categories_recipes` by
contrast has no timestamp, correctly, since category membership isn't
time-ordered anywhere.

Migration `000009_add_recipe_favourites_created_at`:

```sql
-- up
ALTER TABLE recipe_favourites
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- down
ALTER TABLE recipe_favourites DROP COLUMN created_at;
```

### 2. `GET /recipes/favourites` requires full auth, not optional

Unlike `GET /recipes`/`GET /recipes/:id` (`OptionalAuthMiddleware` —
anonymous still gets a useful public result), a favourites list has
nothing to show without an owner. Anonymous → `401`, not an empty list.
This also gives all three new endpoints one consistent auth story:
`POST`/`DELETE` obviously need a real caller (you can't favourite as
nobody), so `GET` matching them means the whole feature is "logged in or
401," not "GET forgiving, write strict."

Route registration — **verified, not assumed**: wrote a throwaway Gin
test registering `GET /recipes/:id` and `GET /recipes/favourites`
together and firing both requests through `ServeHTTP`. No panic at
registration (either order), and `/recipes/favourites` dispatches to the
static handler while `/recipes/abc-123` still hits `:id` — Gin's tree
prioritizes a static segment over a wildcard at the same depth. The
question flagged as open in `CROC-015`'s handoff
(`docs/handoffs/CROC-015.md:445-447`) is closed.

```go
recipes := server.Group("/recipes")
recipes.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess))
{
    recipes.POST("", recipeHandler.Create)
    recipes.GET("/favourites", recipeHandler.ListFavourites)
    recipes.POST("/:id/favourite", recipeHandler.AddFavourite)
    recipes.DELETE("/:id/favourite", recipeHandler.RemoveFavourite)
}
```

### 3. Toggle is idempotent both directions

`POST` on an already-favourited recipe → `200`, still favourited.
`DELETE` on a not-favourited recipe → `200`, still not favourited. A
heart-icon click can double-fire (fast double click, two open tabs, a
retried request after a flaky connection) and none of those are error
conditions — the end state is all that matters, and nothing in this app
needs to detect "tried to favourite twice" as a distinct event. Backed
trivially by the composite PK: `INSERT ... ON CONFLICT (user_id,
recipe_id) DO NOTHING` / a `DELETE` that just affects 0 rows.

### 4. Response body is `{"message": "..."}`, not the recipe

`POST` → `200 {"message": "recipe favourited"}`;
`DELETE` → `200 {"message": "recipe unfavourited"}`. Matches the
project's error-shape rule (`master-spec.md`'s error-shape section):
a success that isn't the created/updated resource returns a message.
A favourite row has no fields worth returning — the frontend already
holds the recipe id/card when the user clicks the heart and flips
`isFavourite` optimistically client-side; it doesn't need a body to act
on. `200`, not `204`, so there's always a body to parse consistently
across both verbs.

### 5. `POST` reuses `CROC-015`'s visibility predicate; `DELETE` does not

`POST /recipes/:id/favourite` on a recipe the caller can't see (someone
else's unapproved recipe) → `404 {"error":"not_found"}`, identical to
`GET /recipes/:id` on the same recipe (`docs/handoffs/CROC-015.md:174-182`).
Otherwise a caller could favourite — and thereby prove the existence of —
a recipe they have no visibility into. Reuses the existing predicate, no
new mechanism:

```sql
-- name: RecipeVisibleToCaller :one
SELECT EXISTS (
    SELECT 1 FROM recipes r
    WHERE r.id = $1
      AND (r.approved
           OR sqlc.arg(caller_is_admin)::boolean
           OR r.created_by_id = sqlc.arg(caller_id)::uuid)
);
```

(No `sqlc.narg` needed here unlike `GetRecipeForReader` — `caller_id` is
never null on this path since the route requires full auth.)

`DELETE` does **not** re-check visibility — it just deletes the
`(userID, recipeID)` row if present. Reasoning: nothing in this codebase
can currently make a previously-visible, previously-favourited recipe
become invisible later (no un-approve, no privacy toggle), so this is a
dormant edge case today — but if it ever happened, blocking the removal
would strand a permanent favourite the user can no longer see or clear.
Removing your own favourite should never depend on still being able to
view the thing.

### 6. `GET /recipes/favourites` — pagination only, no filters

Same envelope as `GET /recipes` (`{recipes, page, limit, total,
totalPages}`, reuse the `page`/`limit` parsing already in
`parseRecipeListFilter`, `internal/handler/recipe_requests.go:245-254`),
but no `q`/`categoryId`/`ingredientId`/`minTime`/`maxTime` — the
favourites-tab screenshot (`screenshots/your crockpot/yp3.png`, "Favourites
tab") shows a plain grid with a count, no filter panel, and a user's own
favourites list will be small. Add filtering later as a small additive
change to the same query if a real need shows up.

Every card in this response has `isFavourite: true` by construction — no
extra per-card lookup needed here, unlike `List`/`GetByID` below.

### 7. `isFavourite` hydration mirrors the existing category-hydration shape

`CROC-015`'s `List`/`GetByID` already hydrate categories via a batched
follow-up query keyed on the page's recipe IDs
(`internal/repository/recipe.go:88-113` for `List`, `:153-160` for
`GetByID`), never touching the main `ListRecipes`/`GetRecipeForReader`
SQL. `isFavourite` follows the same shape rather than joining into those
already-shipped, already-reviewed queries:

- `List`: when `filter.CallerID != nil`, one batched
  `ListFavouritedRecipeIDs(ctx, callerID, recipeIDs)` query returns the
  favourited subset of the page's ids; merge into a `map[uuid.UUID]bool`
  same as `byRecipe` for categories. When `CallerID == nil` (anonymous),
  skip the query entirely — every card's `isFavourite` stays `false`,
  zero extra cost on the common anonymous-browse path.
- `GetByID`: when `callerID != nil`, one
  `IsRecipeFavourited(ctx, callerID, recipeID) (bool, error)` exists
  check. `nil` callerID → `false`, no query.

`models.RecipeCard` gains:

```go
IsFavourite bool `json:"isFavourite"`
```

`RecipeDetail` embeds `RecipeCard`, so it gets the field for free — same
shape both DTOs already share for every other field. Always present
(`false`, never omitted/null) for anonymous callers and for authenticated
callers who haven't favourited.

## Data layer shape

Extend the existing `RecipeRepository` interface
(`internal/handler/recipe_handler.go:16-21`) — one interface across the
epic, per `CROC-014`'s precedent — rather than a new interface, since
`RecipeHandler` already owns every `/recipes` route these three join:

```go
type RecipeRepository interface {
    Create(ctx context.Context, input models.CreateRecipeInput) (*models.Recipe, error)
    CountByCreator(ctx context.Context, userID string) (int, error)
    List(ctx context.Context, filter models.RecipeListFilter) ([]*models.RecipeCard, int, error)
    GetByID(ctx context.Context, id string, callerID *string, callerIsAdmin bool) (*models.RecipeDetail, error)

    AddFavourite(ctx context.Context, userID, recipeID string, callerIsAdmin bool) error // models.ErrRecipeNotFound if hidden/nonexistent
    RemoveFavourite(ctx context.Context, userID, recipeID string) error
    ListFavourites(ctx context.Context, userID string, page, limit int) (cards []*models.RecipeCard, total int, err error)
}
```

`List` and `GetByID`'s signatures don't change — `filter.CallerID` and
`callerID` are already threaded through; the `isFavourite` hydration
happens inside the existing methods.

Recommend a new `internal/sqlc/queries/favourites.sql` (the 5 queries
below) and `internal/repository/favourite.go` rather than growing
`recipes.sql`/`recipe.go` further — mirrors why `recipe_categories.sql`/
`recipe_category.go` are already split out as their own resource, even
though both live under the one `RecipeRepository` interface.

```sql
-- name: AddFavourite :exec
INSERT INTO recipe_favourites (user_id, recipe_id) VALUES ($1, $2)
ON CONFLICT (user_id, recipe_id) DO NOTHING;

-- name: RemoveFavourite :exec
DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2;

-- name: RecipeVisibleToCaller :one
SELECT EXISTS (
    SELECT 1 FROM recipes r
    WHERE r.id = $1
      AND (r.approved OR sqlc.arg(caller_is_admin)::boolean OR r.created_by_id = sqlc.arg(caller_id)::uuid)
);

-- name: ListFavouritedRecipeIDs :many
SELECT recipe_id FROM recipe_favourites
WHERE user_id = $1 AND recipe_id = ANY(sqlc.arg(recipe_ids)::uuid[]);

-- name: IsRecipeFavourited :one
SELECT EXISTS (
    SELECT 1 FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2
);

-- name: ListFavouriteRecipes :many
SELECT r.*
FROM recipes r
JOIN recipe_favourites f ON f.recipe_id = r.id
WHERE f.user_id = $1
ORDER BY f.created_at DESC, r.id
LIMIT $2 OFFSET $3;

-- name: CountFavouriteRecipes :one
SELECT count(*) FROM recipe_favourites WHERE user_id = $1;
```

`AddFavourite`'s repository method: run `RecipeVisibleToCaller` first: if
`false` → `models.ErrRecipeNotFound` (same sentinel `GetByID` already
uses — handler maps it to `404` identically); if `true` → run
`AddFavourite`. Not a transaction — two reads/writes with no invariant
between them that a race would break (worst case of a TOCTOU gap here is
favouriting a recipe the instant it flips from visible to hidden, which
no feature can currently trigger).

`ListFavourites` hydrates categories via the existing
`ListRecipeCardCategories` (`internal/repository/recipe.go:96`) —
identical batched call already used by `List`, just fed this query's IDs
instead.

## Acceptance criteria

- [ ] Migration `000009` applies and reverts cleanly (`migrate up`, `migrate
      down 1`, `migrate up` round-trip against the real dev DB).
- [ ] `POST /recipes/:id/favourite`: `200` + message on first call, `200`
      + message again on a second call (idempotent), `401` anonymous,
      `400` malformed id, `404` hidden/nonexistent recipe.
- [ ] `DELETE /recipes/:id/favourite`: `200` + message whether or not it
      was favourited, `401` anonymous, `400` malformed id. No `404` for a
      recipe you can no longer see but previously favourited.
- [ ] `GET /recipes/favourites`: `401` anonymous; authenticated →
      paginated envelope, every card `isFavourite: true`, ordered by
      favourited-at descending (most recent favourite first); empty list
      for a user with none.
- [ ] `GET /recipes` and `GET /recipes/:id`: `isFavourite` present and
      `false` for anonymous callers and for authenticated callers who
      haven't favourited; `true` after favouriting, flips back to `false`
      after unfavouriting — verified as one real end-to-end sequence
      against the running server, not inferred from the unit tests alone.
- [ ] Recipe deletion (once `CROC-016` ships) cascades to
      `recipe_favourites` with no orphaned rows — already guaranteed by
      the existing `ON DELETE CASCADE` FK
      (`db/migrations/000001_init.up.sql:145-146`), nothing new to build,
      just worth a note that this ticket doesn't need to touch it.
- [ ] `requests/recipes.http` extended with a favourite/unfavourite/list
      section (see Roadmap step 6) and a `Cleanup` addition.

## Non-goals

- No filtering/sorting on `GET /recipes/favourites` beyond pagination —
  see decision 6.
- No live re-check of visibility on every `GET /recipes/favourites` read
  (only at favourite-time via `POST`) — see decision 5. Revisit only if a
  future ticket adds a way for an already-visible recipe to become
  invisible after the fact (un-approve, recipe privacy, etc.) — no such
  ticket exists today.
- No favourites count endpoint/field beyond what pagination's `total`
  already gives (`GET /recipes/favourites`'s envelope) — the
  "Favourites 24" tab badge in the design (`yp1.png`) is a frontend
  concern (`CFE-007`), served by that same `total`.
- No backfill of the old app's `favouriteRecipes` data — deferred at
  `CROC-024`, out of scope here.

## Verification modes

- **Migration** (limits/config-shaped): `migrate up` / `migrate down 1` /
  `migrate up` round-trip against the real dev DB — same check as
  `CROC-032`.
- **Repository layer** (service/API boundary): real requests against the
  real Neon dev DB, per method, via `./scripts/test-repo.sh` — not
  batched at the end. Cover: add/remove idempotency, visibility-gated add
  (visible vs. hidden recipe), the favourited-subset batch query with a
  mix of favourited/unfavourited ids, cascade-on-recipe-delete (a direct
  SQL delete in the test, since `CROC-016`'s endpoint doesn't exist yet).
- **Handler layer** (logic with assertable behaviour): failing test
  first, `testify/mock`-backed `RecipeRepository`, `go test
  ./internal/handler/...`. Cover: 401 anonymous on all three new routes,
  400 malformed id, 404 mapping from `ErrRecipeNotFound`, the message-body
  shape, route dispatch itself (`/recipes/favourites` vs. `/recipes/:id`
  — already verified against real Gin above, but worth a handler-suite
  regression too).
- **`.http` suite**: `requests/recipes.http`, run top-to-bottom against a
  live local server — this is the one place the full
  favourite→appears-in-list→unfavourite→disappears-from-list loop gets
  exercised end-to-end, which the mocked handler tests and the per-method
  repo tests each only see half of.

## Roadmap

1. **Migration**: create `db/migrations/000009_add_recipe_favourites_created_at.{up,down}.sql`
   per decision 1. Run `migrate up`, confirm the column, `migrate down 1`,
   confirm it's gone, `migrate up` again.
2. **sqlc**: add `internal/sqlc/queries/favourites.sql` (the 6 queries
   under Data layer shape above). Run the project's sqlc generate step;
   confirm `internal/sqlc/favourites.sql.go` is produced.
3. **Repository** — write failing tests first in a new
   `internal/repository/favourite_test.go` (follow the shape of
   `internal/repository/recipe_test.go`/`recipe_list_test.go`), confirm
   red, then implement `internal/repository/favourite.go`:
   - `AddFavourite` (visibility check + insert, decision 5)
   - `RemoveFavourite` (unconditional delete, decision 5)
   - `ListFavourites` (join query + category hydration reuse, decision 7)
   - the `isFavourite` hydration added into the existing `List`/`GetByID`
     in `internal/repository/recipe.go` (decision 7) — extend
     `recipe_test.go`/`recipe_list_test.go`'s existing cases rather than
     a new file, since these are the same methods gaining one more field.
   Confirm green against the real dev DB (`./scripts/test-repo.sh`).
4. **Models**: add `IsFavourite bool \`json:"isFavourite"\`` to
   `models.RecipeCard` (`internal/models/recipe.go:38-48`).
5. **Handler** — write failing tests first in `recipe_handler_test.go`
   (mocked `RecipeRepository`), confirm red, then implement in
   `recipe_handler.go`: `AddFavourite`, `RemoveFavourite`,
   `ListFavourites` handler methods (401/400/404 mapping per decisions
   2/3/5), and extend `RecipeRepository`'s interface plus the mock
   (`go tool mockery` regenerate per `CLAUDE.md`'s "re-run after changing
   any interface" rule). Wire the three routes into `main.go`'s existing
   `/recipes` auth group (decision 2's snippet). Confirm green:
   `go test ./internal/handler/...`.
6. **`.http` suite**: extend `requests/recipes.http` with a new section
   mirroring the existing `GET /recipes/:id` section's style — favourite,
   confirm it shows in `GET /recipes/favourites` and `isFavourite: true`
   on `GET /recipes/:id`, unfavourite, confirm both flip back, plus the
   401/400/404 cases from the acceptance checklist. Add a `Cleanup`
   line if any test data needs resetting (likely none — favouriting the
   already-seeded recipe and unfavouriting it at the end of the run
   leaves no residue).
7. **Manual check**: run the `.http` file top-to-bottom against
   `go run .` locally — this is the ticket's real end-to-end verification,
   not a stand-in for it.
8. Hand back for `/code-review medium main`, then close-out.

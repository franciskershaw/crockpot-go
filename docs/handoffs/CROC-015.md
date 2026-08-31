# CROC-015 — Recipe browse/search + detail (read layer)

**Implementation mode: AI-driven.** Piece-by-piece per `crockpot-go/CLAUDE.md`:
failing test → minimal stub (fake sentinels, never a panic) → confirm red
→ stop for go-ahead → implement → confirm green → stop before the next
piece.

**Sequencing:** `CROC-032` (FK indexes) is grilled and built *first* —
this handoff assumes `idx_recipes_created_by_id` and
`idx_recipe_ingredients_item_id` already exist and adds no migration of
its own.

## Summary

`GET /recipes` (paginated, filtered list) and `GET /recipes/:id`
(fully-hydrated detail) — the read contract every recipe surface builds
against (`crockpot-react` CFE-004 browse, CFE-005 detail). Classified
**expensive-to-undo**: the visibility predicate, the two response DTOs,
the filter-param vocabulary, and the new optional-auth middleware all
become load-bearing for CROC-016/017/018 and the frontend.

**Relevance ranking is deliberately split out** into **CROC-042** (its
own grill — scoring model, a probable `recipe_categories` schema change,
match-explanation response fields, random ordering). CROC-015 ships hard
filters + `created_at DESC` ordering only. The requirements gathered for
CROC-042 are captured in the appendix below.

## Decisions from the interview

### 1. Scope — list **and** detail, one ticket

`GET /recipes` + `GET /recipes/:id` as two pieces. They share the
visibility predicate (must be defined once or the endpoints disagree on
who sees an unapproved recipe), the category-hydration layer, and the
`models` read-model changes. CROC-014's handoff defers "hydration" and
"the detail endpoint" to CROC-015 in three places; there is no other
ticket for `GET /recipes/:id` and CFE-005 has nothing else to call.

### 2. Visibility predicate (load-bearing — also in master spec)

A recipe is visible to a caller when:

```sql
WHERE (
  r.approved
  OR (@caller_id::uuid IS NOT NULL AND r.created_by_id = @caller_id)
  OR @caller_is_admin
)
AND (NOT @only_mine
     OR (@caller_id::uuid IS NOT NULL AND r.created_by_id = @caller_id))
```

- **Anonymous** → approved only.
- **Authenticated non-admin** → approved + their own (approved or not).
  A creator's just-submitted pending recipe appears in their browse
  immediately — the expected feedback loop, no privacy reason to hide it
  from its owner. New behaviour: the old app only ever returned
  `approved = true` (`crockpot/src/data/recipes/helper.ts:188`).
- **ADMIN** → everything. Minimum that makes CROC-017's *inline*
  approval flow work without a new endpoint (CFE-014: "no full admin
  panel"). `role` is already on the context from the middleware. A
  focused `?approved=false` pending-queue filter is left to CROC-017.
- **`?mine=true`** → the caller's own only (approved + unapproved), even
  for an admin. Anonymous `?mine=true` → empty result (not 401).

Reused verbatim by CROC-016 (edit/delete authorization), CROC-017
(approve), CROC-018 (favourites list), and later menu/planner reads.
Revisit only if a sharing/visibility feature is added.

### 3. Optional-auth middleware (load-bearing — also in master spec)

New `middleware.OptionalAuthMiddleware(secret)`:

- No `Authorization` header → `c.Next()`, no claims set.
- Malformed header, or an invalid/expired token → **also** `c.Next()`,
  no claims set. **Never aborts, never 401s.**
- Valid token → sets `userID`, `email`, `role` on the context exactly
  as `AuthMiddleware` does.

Handlers detect "anonymous" via the existing `userIDFromCtx`
(`internal/handler/context.go`), which already returns `("", false)`
when `userID` is unset — no handler-side helper needed.

**Invalid token → anonymous, not 401**: a browse page holding an expired
access token still renders the public list; token refresh is the
frontend's separate concern and a read endpoint shouldn't force it.
Reused immediately by CROC-018 (`GET /recipes` needs per-caller
`isFavourite`).

Rejected: two endpoints (`/recipes` public + `/recipes/mine` authed) —
doesn't match the design (pending recipes appear inline in browse) and
`GET /recipes/:id` still needs per-caller visibility regardless.

### 4. List card DTO (`GET /recipes` → `recipes[]`)

```json
{
  "id": "uuid",
  "name": "Slow Cooker Beef Casserole",
  "imageUrl": "https://res.cloudinary.com/...",
  "imageFilename": "abc123",
  "timeInMinutes": 360,
  "serves": 4,
  "approved": true,
  "categories": [{ "id": "uuid", "name": "Batch" }],
  "createdAt": "2026-08-31T..."
}
```

- **No `ingredients` / `instructions` / `notes`** — the card
  (`screenshots/browse/browse1.png`) doesn't render them; per-card
  ingredient hydration across a page is real cost.
- **`categories` hydrated to `{id,name}`**, not bare IDs — the card
  renders name badges; one batched join here beats the frontend joining
  every card against its category cache. Detail returns the same shape
  → one category-hydration story.
- **`approved` included** — the creator's own pending recipes appear in
  their browse (decision 2); they need to know which.
- **No `createdById` / `createdByName`** — the card doesn't show author.
  `created_by_id` drives the visibility `WHERE` server-side only.
- Diverges from CROC-014's create response (`categoryIds` bare IDs — a
  decision explicitly scoped to "redirect-and-refetch after create").
  Harmonising create onto `categories: [{id,name}]` is flagged for
  CROC-016.

### 5. Detail DTO (`GET /recipes/:id`) — `RecipeDetail` embeds `RecipeCard`

```json
{
  "id": "uuid", "name": "...",
  "description": "This one's a freezer-stash regular…",
  "timeInMinutes": 360, "serves": 4,
  "instructions": ["Toss the beef…"],
  "notes": ["Freezes brilliantly…"],
  "imageUrl": "https://res.cloudinary.com/...", "imageFilename": "abc123",
  "approved": true,
  "categories": [{ "id": "uuid", "name": "Batch" }],
  "ingredients": [
    { "itemId": "uuid", "itemName": "beef shin",
      "itemCategoryId": "uuid", "itemCategoryName": "Meat & Fish",
      "unitId": "uuid", "unitAbbreviation": "g", "quantity": 800 },
    { "itemId": "uuid", "itemName": "onions",
      "itemCategoryId": "uuid", "itemCategoryName": "Fruit & Veg",
      "unitId": null, "unitAbbreviation": null, "quantity": 3 }
  ],
  "createdById": "uuid", "createdByName": "Jamie M.",
  "createdAt": "2026-08-31T...", "updatedAt": "2026-09-01T..."
}
```

- **`description` — read now as `string | null`**, returns `null` for
  every existing recipe. Column exists (`000001_init.up.sql:110`), the
  detail design shows it prominently
  (`screenshots/recipe detail/detail1.png`), and a DTO that structurally
  omits it forces CFE-005 to hard-code its absence. **The write path
  (create/update accepting `description`) is a flagged addition to
  CROC-016.**
- **Ingredients fully hydrated server-side**, ordered by `position`
  (submit order, CROC-014 decision 11): `itemName`, `itemCategoryId` +
  `itemCategoryName` (the design's `FRUIT & VEG` / `MEAT & FISH` group
  headers), `unitId` + `unitAbbreviation`. The alternative — frontend
  joining every ingredient against the whole unpaginated `/items`
  catalog + `/units` — is a large client-side join for one screen. The
  old app hydrated this server-side (`getRecipeById.ts:28-71`).
  `items.category_id` is `NOT NULL` (inner join safe);
  `recipe_ingredients.unit_id` is nullable (left join).
- **`ingredients` keeps `itemId` / `unitId`** alongside names — the
  frontend's shopping-list / serves-scaler work against IDs.
- **`updatedAt` exposed on detail** (currently `json:"-"` in
  `models.Recipe`); list stays without it.
- **`RecipeDetail` embeds `RecipeCard`** (Go struct embedding, JSON
  flattens) — ~9 shared fields defined once.

### 6. 404, not 403, for a hidden recipe

`GET /recipes/:id` for an unapproved recipe the caller can't see → the
same `404 {"error":"not_found"}` as a nonexistent id. No "exists but
not yours" signal — matches this codebase's enumeration-defence pattern
(`master-spec.md` error-shape section) and the old app
(`getRecipeById` → `canViewRecipe` → `null` → 404). Malformed (non-UUID)
`:id` → `400 {"error":"invalid_request"}` via `parseID`
(`internal/handler/validation.go`).

### 7. Filter parameter vocabulary (load-bearing — also in master spec)

All optional, repeatable via `c.QueryArray`. **Hard filters** — they
shape the candidate `WHERE`, not a ranking signal.

| Param | Form | Semantics |
| --- | --- | --- |
| `q` | `?q=beef` | `name ILIKE '%' \|\| q \|\| '%'`. Trimmed; empty → ignored. |
| `categoryId` | repeated | see `categoryMode` |
| `categoryMode` | `include` (default) / `exclude` | include: recipe has ≥1 of these; exclude: recipe has **none**. Unknown value → `400 invalid_request`. Resolved **in the handler** by routing the IDs into `include_category_ids` or `exclude_category_ids` — no SQL branching on mode. |
| `ingredientId` | repeated | recipe has ≥1 `recipe_ingredients.item_id` in the set |
| `minTime` / `maxTime` | `?minTime=20&maxTime=90` | `time_in_minutes BETWEEN`. Each optional. |
| `mine` | `?mine=true` | decision 2 |
| `page` / `limit` | decision 8 | |

- **Category-include and ingredient filters are OR-combined** (the
  candidate net): a recipe matches if it hits the include-category set
  **or** the ingredient set. This is what CROC-042's ranking expects —
  score orders the union. In CROC-015 alone the union is just
  date-ordered. `categoryMode=exclude` and `minTime`/`maxTime` are
  always ANDed (hard).
- Malformed UUID in `categoryId`/`ingredientId` → `400 invalid_request`.
  Well-formed but nonexistent → matches nothing, not an error.
- `minTime`/`maxTime` non-numeric → `400 invalid_request`; clamped to
  `[0, 1_000_000]` (negative → 0, and an upper cap so `int32(minTime)`
  in the repo can't wrap — same guard as `page`); `minTime > maxTime` →
  empty result, not an error.

### 8. Pagination (load-bearing — also in master spec)

- Params: `?page=` (1-based, default `1`), `?limit=` (default `20`, max
  `50`).
- Envelope:
  ```json
  { "recipes": [ … ], "page": 1, "limit": 20, "total": 189, "totalPages": 10 }
  ```
  keyed `recipes` (no second paginated list yet to justify a generic
  wrapper). `totalPages = ceil(total/limit)` included — saves the
  frontend an off-by-one.
- `total` = count of what **this caller** can see matching **these
  filters** (respects visibility + every filter).
- Out-of-range `page` (beyond `totalPages`) → `200` with empty
  `recipes`, correct `total`/`totalPages`.
- `page`/`limit` non-numeric → `400 invalid_request`; `page < 1` or
  `limit` outside `[1,50]` → clamped (forgiving read endpoint).
- Offset, not cursor: stable `created_at DESC, id` order makes offset
  paging correct for TanStack `useInfiniteQuery`
  (`getNextPageParam: p => p.page < p.totalPages ? p.page + 1 : undefined`).
  Known caveat: a recipe created mid-scroll shifts later pages by one
  (dup/skip at a boundary) — inherent to offset paging, non-issue at
  this volume, and the old app had it worse under random order.

### 9. Ordering (provisional — CROC-042 owns all ordering)

`ORDER BY created_at DESC, id`. Deterministic → free stable pagination.
No seed param, no random ordering in CROC-015 — CROC-042 designs both
the relevance ordering (filters active) and the random ordering
(low-signal case) together.

### 10. Data-layer shape

**`RecipeRepository`** (grown per CROC-014 precedent — one interface
across the epic):

```go
List(ctx, filter models.RecipeListFilter) (cards []*models.RecipeCard, total int, err error)
GetByID(ctx, id string, callerID *string, callerIsAdmin bool) (*models.RecipeDetail, error) // ErrRecipeNotFound if missing OR hidden
```

**sqlc queries** (`internal/sqlc/queries/recipes.sql`, appended):

- `ListRecipes :many` — one **static** query; all filtering via
  sentinel params (`name_query = ''`, `min_time = 0`, `max_time = 0`,
  `include_category_ids = '{}'`, `exclude_category_ids = '{}'`,
  `ingredient_ids = '{}'`, `caller_id` nullable-UUID, `caller_is_admin`,
  `only_mine`). `ORDER BY created_at DESC, id`, `LIMIT/OFFSET`.
- `CountRecipes :one` — **identical WHERE**, no order/limit.
- `ListRecipeCardCategories :many` — `WHERE recipe_id = ANY($1)`, one
  batched call per page (no N+1), `ORDER BY rc.name`.
- `GetRecipeForReader :one` — `WHERE id = $1 AND (<visibility>)` → zero
  rows ⇒ `ErrRecipeNotFound`.
- `ListRecipeDetailCategories :many` (`WHERE recipe_id = $1 ORDER BY
  rc.name`).
- `ListRecipeIngredientsHydrated :many` — joins `items` (inner),
  `item_categories` (inner), `units` (left); `WHERE recipe_id = $1
  ORDER BY position`.

**Count-drift wart**: sqlc can't share a WHERE fragment between
`ListRecipes` and `CountRecipes`. Mitigation: a repo test runs both
across several filter combos and asserts `len(List, limit=10000) ==
Count`. The old app carried the same duplication (`getRecipes` +
`getRecipeCount` both call `buildWhereClause`). Hand-rolled pgx was
rejected — loses sqlc's generated types, and the project picked sqlc
deliberately for schema-heavy work.

**New model types** (`internal/models/recipe.go`):

- `RecipeCard` — the decision-4 fields.
- `RecipeDetail` — embeds `RecipeCard`, adds `Description *string`,
  `Instructions []string`, `Notes []string`, `Ingredients
  []HydratedIngredient`, `CreatedByID uuid.UUID`, `CreatedByName
  *string`, `UpdatedAt time.Time`.
- `HydratedIngredient` — `ItemID`, `ItemName`, `ItemCategoryID`,
  `ItemCategoryName`, `UnitID *uuid.UUID`, `UnitAbbreviation *string`,
  `Quantity float64`.
- `RecipeListFilter` — parsed params (`Query string`, `IncludeCategoryIDs
  []uuid.UUID`, `ExcludeCategoryIDs []uuid.UUID`, `IngredientIDs
  []uuid.UUID`, `MinTime int`, `MaxTime int`, `Mine bool`, `CallerID
  *string`, `CallerIsAdmin bool`, `Page int`, `Limit int`).
- `CategoryRef{ ID uuid.UUID; Name string }`.
- Existing `models.Recipe` (CROC-014 create response) — **untouched**;
  harmonisation flagged for CROC-016.

`.mockery.yaml` already lists `RecipeRepository`; re-run `go tool
mockery` after adding the methods.

### 11. Routing (`main.go`)

Follows the `recipe-categories` pattern (public GET on `server`, authed
routes in a `Group`):

```go
server.GET("/recipes", middleware.OptionalAuthMiddleware(cfg.JWTSecretAccess), recipeHandler.List)
server.GET("/recipes/:id", middleware.OptionalAuthMiddleware(cfg.JWTSecretAccess), recipeHandler.Get)
// unchanged:
recipes := server.Group("/recipes")
recipes.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess))
{ recipes.POST("", recipeHandler.Create) }
```

Flag for CROC-018: `GET /recipes/favourites` will be a static sibling of
`/recipes/:id` — allowed on current Gin (1.9+), CROC-018 verifies.

## Non-goals

- Relevance ranking, match-explanation fields, "best match" badges,
  random/seeded ordering → **CROC-042**.
- `isFavourite` on list/detail → CROC-018 (additive).
- `description` **write** path → CROC-016.
- Recipe update / delete / approve → CROC-016 / CROC-017.
- `?approved=false` admin pending-queue filter → CROC-017 if wanted.
- `pg_trgm` / full-text search → revisit past a few thousand recipes.
- Harmonising CROC-014's create response (`categoryIds` →
  `categories:[{id,name}]`) → CROC-016.
- Index migration → **CROC-032**, done first.
- Cursor pagination → rejected.
- Any response caching (the old app's `unstable_cache` layers) → not
  ported.

## Acceptance criteria

- [ ] `GET /recipes` and `GET /recipes/:id` behind
      `OptionalAuthMiddleware`; both serve anonymous callers.
- [ ] `OptionalAuthMiddleware`: no header / malformed header /
      expired-or-invalid token all → `c.Next()` with no claims; valid
      token → `userID`/`email`/`role` set; never aborts.
- [ ] Anonymous `GET /recipes` → only `approved` recipes.
- [ ] Authenticated non-admin → approved + own (approved or not); a
      second user does **not** see the first's unapproved recipes.
- [ ] ADMIN → all recipes.
- [ ] `?mine=true` → caller's own only (approved + unapproved);
      anonymous `?mine=true` → empty `recipes`, `200`.
- [ ] `?q=` → case-insensitive partial name match.
- [ ] `?categoryId=` (default/`include`) → recipes with ≥1 of the
      categories; `?categoryMode=exclude` → recipes with none of them;
      unknown `categoryMode` → `400 invalid_request`.
- [ ] `?ingredientId=` → recipes containing ≥1 of the items.
- [ ] `categoryId` (include) + `ingredientId` together → the **union**.
- [ ] `?minTime=`/`?maxTime=` → `time_in_minutes` within range; outside
      → excluded.
- [ ] Malformed UUID in `categoryId`/`ingredientId` → `400
      invalid_request`; well-formed nonexistent → matches nothing, not
      an error.
- [ ] Envelope `{recipes, page, limit, total, totalPages}`; `total`
      respects visibility + all filters; `totalPages = ceil(total/limit)`.
- [ ] `page` beyond `totalPages` → `200`, empty `recipes`, correct
      `total`.
- [ ] `page`/`limit` non-numeric → `400`; `page<1` / `limit` outside
      `[1,50]` → clamped.
- [ ] List order: `created_at DESC`, `id` tiebreak.
- [ ] List card DTO exactly decision 4 (no ingredients/instructions/
      notes; `categories` as `{id,name}`; `approved` present).
- [ ] `GET /recipes/:id` for a visible recipe → decision-5 DTO:
      `description` (`null` currently), ingredients hydrated with
      item/item-category/unit-abbreviation, `position` order, null unit
      → null abbreviation, `categories` `{id,name}`, `createdByName`,
      `updatedAt`.
- [ ] `GET /recipes/:id` for a hidden recipe → `404 not_found`
      (identical to a nonexistent id); malformed `:id` → `400
      invalid_request`.
- [ ] `List` vs `Count` drift guard: `len(List, limit=10000) == Count`
      for several filter combinations.
- [ ] `requests/recipes.http` extended (decision-13 verification).
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| `repository/recipe.go` (`List`, `GetByID`) | Service boundary + assertable logic — test-first, real Neon dev DB, per unit | `./scripts/test-repo.sh -run 'TestListRecipes\|TestGetRecipeForReader'` — visibility matrix (anon / owner / other / admin); every filter + the category-OR-ingredient union; `categoryMode=exclude`; time BETWEEN; `mine`; pagination + `total` under filters; out-of-range page → empty; **count-drift guard**; `created_at DESC, id` order; card category batch-hydration (names, sorted); detail ingredient hydration (item / item-category / unit-abbrev / null unit / `position` order); missing → `ErrRecipeNotFound`; hidden → `ErrRecipeNotFound` |
| `handler/recipe_handler.go` (`List`, `Get`) | Assertable logic — failing test first, mocked `RecipeRepository` | `go test ./internal/handler/...` — param parsing → `RecipeListFilter` (`QueryArray`, `categoryMode` routing, defaults, page/limit clamp, non-numeric → 400, bad UUID → 400); envelope shape; `?mine` + anon → empty 200; detail 400 / 404 / 200; caller id + role threaded from context to the filter |
| `middleware/optional_auth.go` | Assertable logic — failing test first | `go test ./internal/middleware/...` — no header / valid / malformed / expired → `c.Next()`; claims set only when valid; never aborts |
| Full wiring | Interactive — hands-on by the founder | `requests/recipes.http` extended: `GET /recipes` anon happy path + each filter + pagination; `GET /recipes` with `@authToken` showing own-unapproved; `GET /recipes/:id` for approved(anon) / own-unapproved(authed) / other's-unapproved(→404) / malformed(→400) |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff` |
| Review | Gate | `/code-review medium main` |

No visual/screenshot mode — backend only. `screenshots/browse/*` and
`screenshots/recipe detail/*` informed the DTO fields (decisions 4–5);
there is no rendered surface to compare against. The match-explanation
UI has **no screenshot** — CROC-042's concern, out of scope here.

## Piece order (AI-driven)

1. **`middleware/optional_auth.go`** + tests. **Done** — 6 subtests
   green.
2. **`internal/sqlc/queries/recipes.sql`** + `sqlc generate` +
   `models/recipe.go` types + `models/errors.go` (`ErrRecipeNotFound`).
   **Done.**
3. **`repository/recipe.go`** — `List` + `GetByID`, real-DB tests
   (`recipe_list_test.go`, 14 tests incl. the count-drift guard).
   `convert.go` gained `nullableUUIDParam` / `pgUUIDs`. **Done.**
4. **`handler/recipe_handler.go`** — `List` + `Get` + `RecipeListFilter`
   parsing (`parseRecipeListFilter` + helpers in `recipe_requests.go`) +
   interface growth + `go tool mockery`. 13 mocked-repo tests. **Done.**
   `page` clamped `[1, 1_000_000]`, `minTime`/`maxTime` clamped
   `[0, 1_000_000]` — int32-overflow guards (`/code-review` finding).
5. **`main.go`** two `OptionalAuthMiddleware` GET routes +
   `requests/recipes.http` GET section. **Done** — routes smoke-tested
   against a live server; manual `.http` run is the founder's.

Then: lint / format / `go mod tidy -diff` (all clean) → `/code-review
medium main` → `/close-out`.

Completed 2026-08-31.

---

## Appendix — CROC-042 (Recipe relevance ranking): captured requirements

*Not decisions. Input to CROC-042's own grill. Recorded here while the
context is fresh.*

### Why it exists

The core use case — **"I have these ingredients in the house, what can
I cook?"**, especially for anonymous users — needs results ordered by
relevance **and** a visible explanation of *why* each result matched
(which ingredients / categories). The browse reskin
(`screenshots/browse/browse1.png`) does not show match badges, so **the
reskin is incomplete here** — the feature is not cut.

### What's wrong with the old algorithm (do not port it)

The old app (`crockpot/src/data/recipes/relevance-cache.ts`,
`helper.ts`) scored ingredients ×10, categories ×15, name ×20, time ×5,
sorted by score then `createdAt`, with a 5-min relevance cache and a
seeded shuffle for the unfiltered case. Per the founder:

- **Absolute match count ignores recipe size.** 5 matched ingredients
  out of a 100-ingredient recipe should not outrank 3 out of 5 plus a
  correct category. Ranking needs proportion / coverage thinking, and
  probably a **penalty for ingredients the recipe needs that the user
  didn't select** (the "missing ingredients" the user would have to
  buy).
- **Categories are not all the same kind.** The 28 seeded
  `recipe_categories` mix dietary (`Veggie`, `Meaty`, `Fishy`,
  `Healthy`), lifestyle (`Batch`, `Speedy`, `Fun`, `Party`, `Group`),
  cuisine (`Thai`, `Italian`, `Mexican`, `Asian`, `American`), and
  meal-type (`Breakfast`, `Lunch`, `Starters`, `Drinks`). "Exclude
  Meaty" should carry more weight than "exclude Fun" — possibly a hard
  exclude, possibly a heavy negative signal — and the schema currently
  can't express the distinction.
- The Next caching was a workaround for Next's per-request RSC model,
  **not** a design to emulate. Go + a normal connection pool doesn't
  have that problem.

### Open questions for CROC-042's grill

- **Scoring model**: weighting of ingredient coverage vs
  missing-ingredient penalty vs category match vs name vs time. Output:
  absolute score, normalised 0–1 relevance, or discrete tiers
  (great / good / partial)?
- **Where scoring runs**: a Postgres scoring expression in `ORDER BY`
  (set-based, fast, complex) vs fetch the filtered candidate set and
  score in Go (simple, testable, fine at this scale). "Nothing off the
  table."
- **`recipe_categories` schema change**: add a `kind` / `is_dietary`
  marker? Migration + seed-data update. Does `categoryMode=exclude` on
  a dietary category become a hard filter while lifestyle stays a
  signal?
- **Random ordering** (low-signal / no-filter case): must be preserved
  and performant. Seed-param contract (frontend generates per session?
  per load?), Postgres mechanism (`ORDER BY md5(id::text || seed)` or
  similar), interaction with `total` and pagination stability. No
  Next-style cache.
- **Match-explanation response fields**: per-recipe `matchedIngredientIds`,
  `matchedCategoryIds`, `score`/`relevance`, tier? **Blocked on a design
  artifact for the match-explanation UI** — design the data contract
  conservatively or wait for the screenshot.
- **Interaction with CROC-015's hard filters**: `minTime`/`maxTime` stay
  hard; does `q` stay a hard `ILIKE` filter or become a ranking signal
  (or both, as the old app did)?
- When filters are active: order purely by score, or score then
  `created_at` tiebreak?
- Anonymous vs authenticated: any ranking difference? (Probably not.)
- Performance ceiling: at what recipe count does
  fetch-all-candidates-and-score-in-Go stop being acceptable?

### What CROC-042 plugs into

CROC-015 ships the endpoints, DTOs, visibility, filters, and
pagination. CROC-042 replaces the `ORDER BY` and **adds** fields to the
list card DTO (score / matched-ids / tier) — additive, not a rewrite.
The filter-param vocabulary (decision 7) is already shaped for it: the
category-include / ingredient OR-union is the candidate net the score
orders.

Grilled 2026-08-31.

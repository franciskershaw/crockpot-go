# CROC-014 — Recipe creation

**Implementation mode: AI-driven.** Claude implements one piece at a time
per `crockpot-go/CLAUDE.md`'s AI-driven cadence: failing test → minimal
stub that fails for the right reason (fake sentinel values, never a
panic) → confirm red → stop for go-ahead → implement → confirm green →
stop before the next piece.

## Summary

`POST /recipes` — the first Epic 4 ticket and the write contract every
later recipe ticket (CROC-015 browse, CROC-016 update/delete, CROC-017
approve, CROC-018 favourites) builds against. Classified
**expensive-to-undo** at grill time: the request/response shape, the
ingredient + category child-table write pattern, the approval-on-create
rule, and the FREE-tier cap all become load-bearing here.

New territory over CROC-012's many-to-many template: a second child
table (`recipe_ingredients`) written alongside the join table
(`recipe_categories_recipes`) in one transaction, an application-level
allowed-unit check, and the folded-in FREE-tier recipe cap (originally
CROC-023).

One small schema migration (`000007`) — a `recipe_ingredients.position`
column so submit order survives (decision 11). Otherwise `recipes`,
`recipe_ingredients`, `recipe_categories_recipes` are as CROC-002 left
them (`000001_init.up.sql:106-141`), with `recipe_categories_recipes`'s
`category_id` FK already flipped to `ON DELETE RESTRICT` at CROC-013
(`000005`).

## Amendments made during the build (post-grill)

Three points surfaced after the grill, agreed before piece 3:

- **Decision 11 (new)** — `recipe_ingredients.position` column, so
  ingredient submit order is preserved (the old app's embedded array
  had it for free; losing it is a real regression). Migration `000007`,
  `SMALLINT NOT NULL`, table is empty so no backfill. Folded into
  CROC-014 rather than deferred because this is the recipe *write
  contract* and adding the column later means a migration plus reworking
  every read path CROC-015/016 build against it.
- **Decision 5 refinement** — the `image.url` check is not just "valid
  URL": scheme `https` and host exactly `res.cloudinary.com`. Otherwise
  a caller stores an arbitrary third-party URL that every recipe
  viewer's browser fetches. The tighter path-prefix check (must be
  *this* project's cloud, not another Cloudinary customer's) waits for
  CROC-040, which introduces `CLOUDINARY_CLOUD_NAME` config — no new
  config in CROC-014.
- **Decision 8 refinement** — the repository *also* maps the
  `recipe_ingredients_recipe_id_item_id_key` UNIQUE violation to
  `models.ErrRecipeDuplicateIngredient`, not just the FKs. The handler
  still pre-checks duplicates for the clean `400 duplicate_ingredient` +
  early exit, but the repo shouldn't emit a 500-class error for a
  constraint it knows about (a future CROC-016 / CROC-024 caller might
  not dedup).

Separately: the Cloudinary **signed-upload signature endpoint** that
makes `image` fields usable end-to-end is its own ticket, **CROC-040**
(not folded in — new config + its own grill points). CROC-014's `POST
/recipes` contract is unchanged by it.

## Decisions from the interview

### 1. CROC-023 (FREE-tier cap) is folded into this ticket

CROC-023 was already reduced at CROC-010's grill to "a `role`-keyed
limit lookup + owned-recipe count backing Epic 4's FREE ≤5 cap." That
residue is ~15 lines with exactly one caller (this endpoint), in the
same epic. Folding it in avoids a separate grill/handoff/PR for a helper
that can't be exercised without `POST /recipes` anyway. CROC-023 is
marked delivered-by-CROC-014 in the spec.

### 2. Cap semantics

- **`409 {"error": "recipe_limit_reached"}`** once a FREE user is at the
  limit. 409 not 403 (the old app used `403 ForbiddenError`): the user
  *may* create recipes, they just have too many right now — deleting one
  or upgrading resolves it. `snake_case_code` per `master-spec.md`'s
  error-shape rule.
- **Limit map `{"FREE": 5}`; absence = uncapped.** PREMIUM, PRO, ADMIN
  all fall through to uncapped — matches `master-spec.md`'s tier table
  (PREMIUM "unlimited custom recipes"). PRO being uncapped is harmless
  (no v1 user can hold that role).
- **Count = `SELECT count(*) FROM recipes WHERE created_by_id = $1`**,
  approved *and* unapproved. Matches the old app's `getUserRecipeCount`
  (no `approved` filter): the cap is about how much a user has
  submitted, else 5 pending recipes would let them submit unlimited more
  while waiting for approval.
- **Role read from the JWT claim**, no DB round-trip for it (the count
  query is needed regardless).
- **TOCTOU gap accepted and documented.** Two concurrent creates at
  count 4 can both pass → user ends at 6. Same gap the old app has;
  matches `PACK-027`'s "no compliance driver for this personal app"
  reasoning. Closing it means a locking count inside the insert
  transaction — real complexity for a soft courtesy limit, not a
  billing boundary. Revisit when billing (Epic 11) ties money to it.
- **Check order:** bind → validate all fields (no DB) → cap check
  (`CountByCreator`, outside the transaction) → `WithinTx`(recipe →
  ingredients → categories).

### 3. Ingredients reference the curated catalog only — no inline item creation (Path 3)

Each ingredient is `{itemId, unitId?, quantity}`. `itemId` must already
exist in `items`; `unitId` (optional) must exist in `units` if present.
No creating `items` rows from a recipe payload.

Reasoning:
- `master-spec.md`'s ownership model is explicit — `items` is
  "system-owned and admin-managed only." Letting any FREE user mint
  `items` rows by typing an ingredient is the packing-list-go
  "users extend the catalog" model this project deliberately rejected.
- The old app hard-requires every `itemId` to pre-exist
  (`validateRecipeReferences`, `crockpot/src/lib/security.ts:181-228`).
- The "New" badge in `screenshots/add recipe/ar3.0.png` (beef shin,
  Worcestershire sauce) is design-reskin UX that `CLAUDE.md` flags as
  non-final; CROC-012's handoff already parked new-item-during-recipe
  UX as "CROC-014's concern *if* needed, not assumed."

**The gap this leaves** — a user needs an ingredient the catalog
doesn't have — is filled by a follow-up ticket (**CROC-039**,
"user-suggested items via pending-approval"), which mirrors the
`recipes.approved` model onto `items`: a user can mint a *pending* item
usable in their own recipe immediately, invisible in everyone else's
picker until an admin approves/merges it. That mechanism does **not
change CROC-014's contract** — every `recipe_ingredients` row still
carries a real `item_id`; the id just might point at a not-yet-approved
row. Until CROC-039 lands, the founder (admin) adds missing items via
CROC-012's `/items` API.

**Sequencing consequence to honour:** the ticket that opens signup to
real FREE users, or ships the frontend recipe-create form, is sequenced
*after* CROC-039 — otherwise real users hit "item not found" walls.

**Rejected — free-text ingredient fallback** (nullable `item_id` +
`raw_text` on `recipe_ingredients`): guts shopping-list aggregation
(Epic 6), which is the app's core value prop ("auto-generate a shopping
list ... aggregated by ingredient"). "2 onions" + "3 onions" only sum
if both are `item_id = <onions>`.

### 4. Ingredient field rules

- **`unitId` nullable per ingredient** — matches the schema
  (`recipe_ingredients.unit_id` nullable) and the design (`ar3.0.png`
  shows "–" for onions, carrots, bay leaves — count-based, no unit) and
  the old app's optional/nullable `unitId`.
- **`quantity` required, `> 0`** → `400 {"error": "invalid_quantity"}`
  if missing or ≤ 0. A negative/zero number is a well-formed request
  that's semantically wrong (the CROC-012 "malformed vs
  well-formed-but-invalid" distinction), hence its own code, not the
  generic `invalid_request`. `NUMERIC(10,2)` — a submitted `0.333` is
  silently rounded to `0.33` by Postgres; accepted, 2dp is fine for
  cooking quantities, no pre-validation.
- **Allowed-unit enforcement (new — the old app has none, because
  `allowedUnitIds` was added late there, per the founder):** if
  `unitId` is provided *and* the item has a non-empty
  `item_allowed_units` set, `unitId` must be in that set → else
  `400 {"error": "unit_not_allowed_for_item"}`
  (`models.ErrIngredientUnitNotAllowed`).
  - **Empty `item_allowed_units` for an item = no constraint** — any
    existing unit accepted. The old app's `allowedUnitIds` defaults to
    empty meaning "unconstrained" (its "use category defaults" comment
    doesn't port — no category-level default units in this schema), and
    a large fraction of items will still have empty sets after CROC-024.
    "If the admin explicitly constrained it, respect that; else allow
    anything" bounds the blast radius to genuine admin data-quality
    mistakes, fixable via `PATCH /items/:id`.
  - **`unitId` null = always accepted** regardless of the allowed set —
    a count-based ingredient isn't violating a unit constraint.
  - **Implementation:** one batched query —
    `ListItemAllowedUnitIDsForItems` already exists
    (`internal/sqlc/queries/items.sql`), built for this shape — fetch
    allowed units for all the recipe's itemIds at once, validate in Go
    inside the transaction. No per-ingredient round-trip. Lives in the
    repository (needs DB data), returns the domain error.

**Consequence to record on CROC-024:** `item_allowed_units` data
quality is now load-bearing for recipe creation. The migration must
import `allowedUnitIds` faithfully — an item with a bad *partial*
allowed set will block a legit unit.

### 5. Field validation bounds — port the old app's `createRecipeSchema` verbatim

(`crockpot/src/lib/validations.ts:102-131` — already-lived-with product
decisions.)

| Field | Rule | Error code |
| --- | --- | --- |
| `name` | trim, 3–100 chars | `name_required` / `name_too_short` / `name_too_long` |
| `timeInMinutes` | integer, 1–1440 | `invalid_time` |
| `serves` | integer, 1–50 | `invalid_serves` |
| `instructions` | array, 1–50 items, each non-empty after trim | `instructions_required` / `too_many_instructions` / `invalid_instruction` |
| `notes` | array, 0–10 items (optional, default `[]`); empty elements silently dropped after trim | `too_many_notes` |
| `categoryIds` | array, 1–3 items, no duplicates | `categories_required` / `too_many_categories` / `duplicate_category` |
| `ingredients` | array, 1–50 items, no duplicate `itemId` | `ingredients_required` / `too_many_ingredients` / `duplicate_ingredient` |
| `image` | optional; if present, both `url` and `filename` (non-empty) required; `url` scheme `https`, host exactly `res.cloudinary.com` (path-prefix-to-own-cloud check deferred to CROC-040) | `invalid_image` |

- **`name` min-3 needs a recipe-specific `validateRecipeName`** — the
  shared `validateName` helper (`handler/validation.go`) only checks
  empty + >100, and item/unit/category names use it with different
  minimums (2). Don't retrofit the shared helper.
- **Duplicates rejected, not silently de-duped** — a duplicate in the
  payload means the client is confused; silently accepting hides a
  frontend bug and the DB would violate the PK/UNIQUE constraint anyway.
  Clean 400 in the handler, CROC-012 pattern.
- **`instructions` empty element → reject** (`invalid_instruction`): an
  empty numbered step is a broken recipe. **`notes` empty element →
  silent drop**: a blank note row is just noise, dropping is friendlier
  and notes carry no structural meaning.

### 6. Approval, `created_by_name`, who can create

- **`approved = (role == "ADMIN")`** from the JWT claim. Non-admin
  (FREE/PREMIUM/PRO) → `false`. Matches `master-spec.md` ownership model
  and the old app's `isApproved = user.role === "ADMIN"`.
- **`created_by_name` populated by the repository in the INSERT via a
  subquery** — `created_by_name = (SELECT name FROM users WHERE id =
  $createdBy)`. The column exists so the recipe survives `created_by_id`
  going NULL on user deletion (`ON DELETE SET NULL`). JWT claims don't
  carry `name`. A subquery avoids injecting a `UserRepository` into
  `RecipeHandler` for one field, and avoids the bigger change of adding
  `name` to token issuance across both auth paths. `users.name` is
  nullable — a name-less account writes `NULL`, fine (column nullable).
- **Any authenticated user can create** — `POST /recipes` behind
  `AuthMiddleware` only, no `RequireRole`. FREE is the floor (spec:
  "up to 5 own custom recipes"), so the only gate is the count cap.
  Email-verification isn't a separate check: password users can't get
  an access token without verifying, Google users are verified by
  Google.

### 7. Response shape (201) — the created recipe, bare, relations as ID arrays

```json
{
  "id": "uuid",
  "name": "Slow Cooker Beef Casserole",
  "timeInMinutes": 360,
  "serves": 4,
  "instructions": ["Toss the beef...", "..."],
  "notes": ["Freezes brilliantly..."],
  "imageUrl": "https://...",
  "imageFilename": "abc123",
  "approved": false,
  "categoryIds": ["uuid", "uuid"],
  "ingredients": [
    { "itemId": "uuid", "unitId": "uuid", "quantity": 800 },
    { "itemId": "uuid", "unitId": null, "quantity": 3 }
  ],
  "createdById": "uuid",
  "createdByName": "Jane M",
  "createdAt": "2026-08-31T..."
}
```

- **ID arrays, not hydrated objects** — same call as CROC-012 decision
  1. `crockpot-react` redirects to `/recipes/:id` after create and
  refetches; hydration (category names, item names, unit abbreviations)
  belongs to CROC-015's detail endpoint. ids→hydrated later is a pure
  additive change.
- **`ingredients` and `categoryIds` echoed back** — confirms what
  persisted (quantity rounding, null `unitId` accepted).
- **`createdByName` included** — it's a column on the row just written.
- **`description` omitted entirely** — not in the CROC-014 field list;
  the column stays nullable/unused.
- **No `favouritedByIds`/`isFavourite`** — CROC-018.
- **`camelCase` JSON keys** — matches every existing handler and the old
  app.
- **Bare resource on create** — matches `master-spec.md`'s success-shape
  rule and CROC-010–013.

### 8. Repository & data-layer shape

**Interface** (`handler` package, consumer-defined):

```go
type RecipeRepository interface {
    Create(ctx context.Context, input CreateRecipeInput) (*models.Recipe, error)
    CountByCreator(ctx context.Context, userID string) (int, error)
}
```

One interface, grown across the epic (CROC-015–017 add List/Get/Update/
Delete/Approve) — matches `RefreshTokenRepository`'s "one interface
split across three tickets" precedent. `CountByCreator` stays on it:
one consumer (`RecipeHandler`), one implementation, both methods evolve
together — segregating buys nothing at this scale.

`CreateRecipeInput` is a `handler`-package struct of validated
primitives: `Name string`, `TimeInMinutes int`, `Serves int`,
`Instructions []string`, `Notes []string`, `CategoryIDs []uuid.UUID`,
`Ingredients []Ingredient{ItemID uuid.UUID, UnitID *uuid.UUID, Quantity
<decimal>}`, `ImageURL *string`, `ImageFilename *string`, `CreatedByID
uuid.UUID`, `Approved bool`.

**`internal/sqlc/queries/recipes.sql`** (new):
- `CreateRecipe :one` — insert `recipes`, `created_by_name` via the
  `(SELECT name FROM users WHERE id = $createdBy)` subquery,
  `RETURNING *`
- `CreateRecipeIngredient :exec` — one row incl. `position`, called in a
  loop with the slice index as `position` (small counts, matches
  `CreateItemAllowedUnit`)
- `CreateRecipeCategoryLink :exec` — one `(recipe_id, category_id)` row,
  loop
- `CountRecipesByCreator :one` — `SELECT count(*) FROM recipes WHERE
  created_by_id = $1`
- `ListRecipeIngredients :many` (`ORDER BY position`) /
  `ListRecipeCategoryIDsForRecipe :many` — for `Create`'s own return
  hydration, scoped to the one recipe
- reuse existing `ListItemAllowedUnitIDsForItems` for the allowed-unit
  check

**`internal/models/recipe.go`** (new) — `Recipe` + `Ingredient` +
`CreateRecipeInput` structs (the input DTO lives in `models`, not
`handler`, since neither package imports the other). `toModelRecipe`
converter. Quantity: `pgtype.Numeric` ↔ `float64` at the model boundary
via `numericParam`/`numericValue` in `repository/convert.go` (string
round-trip; no decimal lib in the tree — confirmed).

**`internal/models/errors.go`** additions — `ErrRecipeLimitReached`,
`ErrRecipeInvalidItem`, `ErrRecipeInvalidUnit`, `ErrRecipeInvalidCategory`,
`ErrIngredientUnitNotAllowed`, `ErrRecipeDuplicateIngredient`. FK
violations on `item_id`/`unit_id`/`category_id` and the
`recipe_ingredients_recipe_id_item_id_key` UNIQUE violation → the
corresponding errors via the shared `pgConstraintError` helper
(constraint names confirmed live: `recipe_ingredients_item_id_fkey`,
`recipe_ingredients_unit_id_fkey`,
`recipe_categories_recipes_category_id_fkey`,
`recipe_ingredients_recipe_id_item_id_key`). The allowed-unit check →
`ErrIngredientUnitNotAllowed`.

**Transaction:** `RecipeHandler` takes the existing `Transactor` (like
`ItemHandler`). `Create` does `CountByCreator` *outside* the tx, then
`WithinTx`: batched allowed-unit check (fully before any write, so a
disallowed unit fails cleanly with nothing inserted) → insert recipe →
insert ingredients → insert category links. All `queriesFor(ctx, r.db)`,
no repo-internal `Begin`. Consequence of check-then-insert: for a
*constrained* item, a `unitId` that doesn't exist returns
`ErrIngredientUnitNotAllowed`, not `ErrRecipeInvalidUnit` — the FK is
only the authority for the *unconstrained*-item case.

**`.mockery.yaml`** — add `RecipeRepository`, re-run `go tool mockery`.

### 9. Route wiring + `.http`

```go
recipes := server.Group("/recipes")
recipes.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess))
{
    recipes.POST("", recipeHandler.Create)
}
```

`NewRecipeHandler(recipeRepo, transactor)` wired in `main.go` alongside
the others.

**`requests/recipes.http`** (new) — follows `items.http`'s shape: a
`Login` against the seeded (ADMIN-bumped) test account to capture
`@authToken`, fixture ids from `GET /items` and `GET /recipe-categories`,
then: create happy path (`approved: true` with the ADMIN account), the
400 validation + invalid-reference cases, a Cleanup section (raw
`DELETE FROM recipes WHERE name LIKE 'http-test-%'` note until CROC-016
adds a delete endpoint). Part of this ticket's AC (`CLAUDE.md`
manual-suite rule). The FREE-cap 409 and non-admin `approved: false`
paths aren't reachable with the ADMIN account — covered at the handler
layer with mocked claims.

### 10. `notes` / `instructions` storage

- **`notes` always stored as a non-null array** (`[]` when
  omitted/empty) — matches the old app's `notes: data.notes || []`,
  keeps CROC-015 reads free of null-vs-empty branching.

## Non-goals

- Inline item creation from a recipe payload — decision 3, CROC-039's
  job.
- Free-text / unstructured ingredients — decision 3, rejected.
- Recipe update, delete — CROC-016.
- Recipe browse/search/detail and relation hydration — CROC-015.
- Admin approval endpoint — CROC-017.
- Favourites — CROC-018.
- The `description` column — not in scope, stays unused.
- Ingredient "paste one per line" parsing and link-import population —
  frontend / CROC-026 (Premium), and resolved at review-time before any
  POST.
- Closing the cap TOCTOU gap — decision 2, revisit at Epic 11.
- Enforcing allowed-unit membership when an item's allowed set is empty
  — decision 4, deliberate.
- The Cloudinary signed-upload signature endpoint — CROC-040. CROC-014
  only validates + stores the two strings; without CROC-040 the frontend
  has no authorised way to produce them.
- Orphaned Cloudinary images (abandoned uploads, deleted/replaced
  recipes) — CROC-016's grill.

## Acceptance criteria

- [ ] `POST /recipes` behind `AuthMiddleware` only; no token → 401; no
      `RequireRole`.
- [ ] Valid payload from a **non-admin** token → 201, `approved: false`,
      response matches the decision-7 shape (ids echoed, `createdByName`
      populated from the `users` row).
- [ ] Valid payload from an **ADMIN** token → 201, `approved: true`.
- [ ] `recipe_ingredients` rows written for every ingredient, including
      one with `unitId: null`; `recipe_categories_recipes` rows written
      for every category; all in one transaction.
- [ ] Ingredients come back in submit order (`position` column); a
      recipe created with ingredients [C, A, B] reads back [C, A, B],
      not sorted by `item_id`.
- [ ] Duplicate `itemId` reaching the repository (handler check
      bypassed) → `ErrRecipeDuplicateIngredient`, not a 500-class error.
- [ ] `image.url` that is a valid URL but not `https` + host
      `res.cloudinary.com` → `400 invalid_image`.
- [ ] `created_by_name` on the persisted row equals the creator's
      current `users.name`.
- [ ] FREE user with 5 existing own recipes (approved or not) → `409
      recipe_limit_reached`; FREE user with 4 → 201; PREMIUM / ADMIN
      with ≥5 → 201.
- [ ] Nonexistent `itemId` → `400 invalid_item_id`; nonexistent
      `unitId` → `400 invalid_unit_id`; nonexistent `categoryId` →
      `400 invalid_category_id`; malformed (non-UUID) any of them →
      `400 invalid_request`.
- [ ] `unitId` set that is not in the item's non-empty
      `item_allowed_units` → `400 unit_not_allowed_for_item`; `unitId`
      in the set → 201; item with an empty allowed set + any real unit →
      201; `unitId: null` → 201 regardless.
- [ ] All decision-5 validation bounds enforced with their specific
      error codes; `name` 3–100; duplicate `categoryId` →
      `duplicate_category`; duplicate `itemId` → `duplicate_ingredient`;
      empty instruction element → `invalid_instruction`; empty note
      element silently dropped; `quantity` ≤ 0 → `invalid_quantity`.
- [ ] `image` omitted → 201, `imageUrl`/`imageFilename` null; `image`
      with only one of url/filename → `400 invalid_image`.
- [ ] A failed create (e.g. bad `unitId` on the 3rd of 5 ingredients)
      leaves no partial `recipes` row — proves the `Transactor` wrapping
      works, not just compiles.
- [ ] `requests/recipes.http` added, following `items.http`'s shape.
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| `repository/recipe.go` | Service boundary — test-first, real Neon dev DB, per unit | `./scripts/test-repo.sh -run 'TestCreateRecipe\|TestCountRecipesByCreator'` — create (recipe row + `created_by_name` subquery; ingredients incl. null `unitId`; category links; submit-order via `position`); `CountRecipesByCreator` (approved + unapproved); FK on bad `item_id`/`unit_id`/`category_id` → domain errors; UNIQUE `(recipe_id, item_id)` → `ErrRecipeDuplicateIngredient`; allowed-unit check (in-set OK, out-of-set → `ErrIngredientUnitNotAllowed`, empty set unconstrained, null unit skips); rollback leaves no partial `recipes` row on a mid-loop failure |
| `handler/recipe_handler.go` | Logic with assertable behaviour — failing test first, mocked `RecipeRepository` + mocked `Transactor` | `go test ./internal/handler/...` — all decision-5 validation cases → specific 400s; `approved` true for ADMIN claim / false otherwise; cap: FREE at 5 → 409, FREE at 4 / PREMIUM / ADMIN → pass; repo domain errors → status translations; dedup `categoryIds`/`itemId` → 400 |
| Full wiring | Interactive — hands-on by the founder | `requests/recipes.http` top-to-bottom in VS Code REST Client against a live server + real DB: `Login` (ADMIN test account) → create happy path (`approved: true`), the 400 validation + invalid-reference translations, Cleanup |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff` |
| Review | Gate | `/code-review medium main` |

No visual/screenshot mode — backend only. `screenshots/add recipe/*`
informed the request contract (decisions 4–5); there is no rendered
surface to compare against.

## Piece order (AI-driven)

1. **`internal/sqlc/queries/recipes.sql`** + `db/migrations/000007_add_recipe_ingredient_position`
   + `sqlc generate` + `models/recipe.go` (`Recipe`, `Ingredient`,
   `CreateRecipeInput`) + `models/errors.go` entries. Mechanical;
   generated code isn't TDD-stubbed — step 2's repo tests cover it.
   **Done** (commits `d98035e`, plus the `000007` amendment). Constraint
   names and the `NUMERIC(10,2)` → `pgtype.Numeric` type confirmed live.
2. **`repository/recipe.go`** — real-DB repo tests, stub with fake
   sentinels, red → stop → green → stop. Covers `Create` (check-then-insert
   allowed-unit check, `position`, FK + UNIQUE → domain errors) and
   `CountByCreator`. **Done** (commits `940317b` + green diff).
3. **`handler/recipe_handler.go`** + `recipe_requests.go` +
   `RecipeRepository` interface — mocked-repo handler tests, stub → red
   → stop → green → stop. **Done** (commit `b75eed9` + green + a
   follow-up refactor: request DTOs and validators split into
   `recipe_requests.go`, `Create` reduced to orchestration calling
   `parseCreateRecipeInput` / `withinRecipeCap`; `CROC-041` filed for
   the codebase-wide `badRequest(c, code)` helper).
4. **Wire `main.go`** (new authed `/recipes` group, `NewRecipeHandler`
   with the existing `transactor`) + `requests/recipes.http`. **Done** —
   awaiting manual `.http` verification (founder) then
   `/code-review medium main`.

## Master-spec changes made alongside this handoff

- **Epic 4 / CROC-014** — expanded from the one-line backlog entry to
  the acceptance block (contract, cap, allowed-unit rule, non-goals,
  verification pointer to this doc).
- **Epic 7 / CROC-023** — marked "Delivered by CROC-014" with a pointer
  here; not deleted (keeps the number stable and the history legible).
- **Epic 4 / CROC-039** (new) — "user-suggested items via
  pending-approval": mirrors `recipes.approved` onto `items` so a user
  can propose a catalog item during recipe creation without
  admin-gating the whole flow. Records the rejected free-text
  alternative and the sequencing rule (must land before real FREE-user
  signup / the frontend recipe-create form).
- **Epic 8 / CROC-024** — note added: the migration must import
  `allowedUnitIds` faithfully, as `item_allowed_units` data quality is
  now load-bearing for recipe creation (decision 4).
- **Architecture / Images** — rewritten to "**signed** client-side
  direct upload", records the rejected unsigned-preset option and the
  image-URL validation rule for every URL-accepting endpoint.
- **Epic 4 / CROC-040** (new) — Cloudinary signed-upload signature
  endpoint (`POST /uploads/signature`). Dependency of `crockpot-react`
  CFE-010; CROC-014's `image` fields are inert without it. Same
  pre-real-signup sequencing gate as CROC-039.
- **Epic 4 / CROC-016** — open question added: orphaned Cloudinary
  images on recipe delete / image replace (the old app's server-side
  `deleteRecipeImage` has no Go-API equivalent).

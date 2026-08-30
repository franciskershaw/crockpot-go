# CROC-013 — Recipe categories CRUD

**Implementation mode: AI-driven.** Claude implements one piece at a time
per `crockpot-go/CLAUDE.md`'s AI-driven cadence: failing test → minimal
stub that fails for the right reason (fake sentinel values, never a
panic) → confirm red → stop for go-ahead → implement → confirm green →
stop before the next piece.

## Summary

`/recipe-categories` — third and last of the Epic 3 reference-data CRUD
resources (after `item_categories` CROC-010, `units` CROC-011). Same
template throughout: `GET` public, `POST`/`PATCH`/`DELETE` ADMIN-only via
`middleware.RequireRole("ADMIN")`, `handler/{errors,validation}.go`
helpers, `pgConstraintError`/`pgErrorCode` repo helpers (the shared
functions CROC-012 factored out of the three near-duplicate repos).
`recipe_categories` has a single `name` field — no `icon`/`abbreviation`
sibling column, so there is no partial-PATCH branching: `PATCH` always
requires `{name}`.

One genuine new decision, not just template reuse: `recipe_categories`
→ `recipe_categories_recipes` is `ON DELETE CASCADE` in
`000001_init.up.sql`, unlike `item_categories`/`units`, whose referencing
FKs are `ON DELETE RESTRICT`. This ticket flips it to RESTRICT.

## Decisions from the interview

### 1. Delete-in-use — flip `recipe_categories_recipes`'s FK to RESTRICT

`db/migrations/000001_init.up.sql:135-138` declared
`category_id UUID NOT NULL REFERENCES recipe_categories (id) ON DELETE
CASCADE`. As shipped, deleting a category silently unlinks it from every
recipe tagged with it — no error, no signal, `204` regardless. Checked:
unlike `item_allowed_units`'s CASCADE (CROC-010/011, deliberate and
documented — a soft item↔unit association, not core categorization),
nothing in `master-spec.md` or the schema comments calls this CASCADE
intentional; it reads as an unconsidered default from the initial schema
pass.

**Decision:** new migration flips it to `ON DELETE RESTRICT`. Deleting a
category that's still tagged on any recipe now fails with `409
{"error": "category_in_use"}`, caught the same way as CROC-010/011:
Postgres `23001` (`restrict_violation`, confirmed live against Neon at
CROC-010 — PG 15+ splits RESTRICT into its own SQLSTATE, not `23503`)
via the shared `pgErrorCode(err, pgerrcode.RestrictViolation)` helper.

Reasoning:
- An admin deleting "Desserts" while dozens of recipes carry that tag is
  much more likely a mistake than an intended bulk-untag; CASCADE fails
  that admin silently, RESTRICT forces them to see the conflict and
  recategorize first.
- Matches the established shape for "reference data an owned record
  depends on" (`item_categories`↔`items`, `units`↔`recipe_ingredients`/
  `shopping_list_items`) — `recipe_categories`↔`recipes` is the same
  shape, not the `item_allowed_units` shape.
- No new code path: same `pgConstraintError`/`pgErrorCode` helpers,
  same `409 category_in_use` response contract as the other two
  resources.
- Rejected: leaving CASCADE. Silent data loss on a destructive admin
  action, and it would make `recipe_categories` the only reference-data
  resource in the codebase without an in-use guard, with no design note
  explaining the asymmetry.

**Consequence for CROC-014-018:** recipe creation/update must handle
`409 category_in_use` from category *deletion* meaning nothing to them
directly (that's an admin write path); but it does mean recipes can
never be left with a dangling category reference — an admin cannot
delete a category out from under an existing recipe. No schema change
needed on the recipe side.

### 2. Endpoint surface

| Method | Path | Auth | Success | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/recipe-categories` | public | 200 | `[{id, name, createdAt}]`, ordered by `name` |
| `POST` | `/recipe-categories` | ADMIN | 201 | the created resource, bare |
| `PATCH` | `/recipe-categories/:id` | ADMIN | 200 | the updated resource, bare |
| `DELETE` | `/recipe-categories/:id` | ADMIN | 204 | none |

- **No `GET /recipe-categories/:id` route** and **no `recipeUsageCount`
  in the payload** — same reasoning as CROC-010's `item_categories`:
  every consumer needs the whole list (a handful of rows); the old
  app's admin table showed `recipeUsageCount`
  (`crockpot/src/app/admin/(tabs)/recipe-categories/utils/
  recipeCategoryColumns.ts`), but `master-spec.md:62-63` states
  crockpot-react has no admin CRUD UI for categories in v1 — nothing
  consumes either. Add both if a real admin-UI ticket lands.
- **`PATCH` is not partial** — the resource has one mutable field
  (`name`), so `{name}` is simply required, not optional-one-of-several
  like `item_categories`'s `{name?, icon?}`. Missing/blank → `400
  {"error": "name_required"}` via the existing `bindJSON` +
  `validateName` helpers, no new validation code.
- Bare resource on create/update, matches `master-spec.md`'s
  success-shape rule and CROC-010/011.

### 3. Conflict + not-found — reuse the established shapes exactly

- `POST`/`PATCH` unique-violation on `recipe_categories_name_key`
  (`23505`) → `409 {"error": "name_taken"}`, via
  `pgConstraintError(err, recipeCategoryConstraintErrors)` (same helper
  CROC-012 factored out, new constraint-name map entry only).
- `DELETE`/`UPDATE ... RETURNING` → `pgx.ErrNoRows` →
  `models.ErrRecipeCategoryNotFound` → `404 {"error": "not_found"}`.
- No pre-check `SELECT` anywhere — DB is the source of truth, same
  race-free reasoning as CROC-010 decision 4/5.

### 4. Seed data — MongoDB export, same founder-action step as CROC-010/011

The old app's `RecipeCategory` collection (`crockpot/prisma/
schema.prisma:96-104`, `{name}` only, no icon) has real production
category names. Seeded here the same way `item_categories`
(CROC-010) and `units` (CROC-011) were: a founder-action MongoDB export
turned into an idempotent `INSERT ... ON CONFLICT (name) DO NOTHING`
seed migration.

Reasoning: keeps real category names in place before CROC-014 (recipe
creation) needs them to tag recipes — avoids an admin hand-typing every
category name back in through the API after the fact. Same
reversibility profile as the other two seeds (down migration deletes
exactly the seeded `name`s).

### 5. Implementation mode — AI-driven

Fourth application of a fully-established template (`RequireRole`
already built at CROC-010; handler/validation helpers already built;
`pgConstraintError`/`pgErrorCode` repo helpers already built at
CROC-012). No new stack territory — this is routine template
application, same reasoning as CROC-011/012's AI-driven calls.

## Schema + data changes

- `000005_restrict_recipe_categories_recipes.up.sql` — `ALTER TABLE
  recipe_categories_recipes DROP CONSTRAINT
  recipe_categories_recipes_category_id_fkey, ADD CONSTRAINT
  recipe_categories_recipes_category_id_fkey FOREIGN KEY (category_id)
  REFERENCES recipe_categories (id) ON DELETE RESTRICT;` (constraint
  name confirmed against the generated name Postgres assigns a
  column-level `REFERENCES` clause — verify exact name via `\d
  recipe_categories_recipes` in the dev DB before writing the migration,
  do not assume). `.down.sql` reverses to `ON DELETE CASCADE`.
- `000006_seed_recipe_categories.up.sql` / `.down.sql` — idempotent seed
  (`ON CONFLICT (name) DO NOTHING`) from the founder's MongoDB export;
  down deletes exactly those `name`s.
- `internal/sqlc/queries/recipe_categories.sql` — `ListRecipeCategories`,
  `CreateRecipeCategory`, `UpdateRecipeCategory`, `DeleteRecipeCategory`
  (`... RETURNING id`). No `COALESCE(NULLIF(...))` partial-update
  pattern needed (single field, always required per decision 2).
- Regenerate: `sqlc generate` (adds `RecipeCategory` to
  `internal/sqlc/models.go` + `recipe_categories.sql.go`).
- `internal/models/recipe_category.go` — `RecipeCategory{ ID uuid.UUID,
  Name string, CreatedAt time.Time }`.
- `internal/models/errors.go` — `ErrRecipeCategoryNotFound`,
  `ErrRecipeCategoryInUse`, `ErrRecipeCategoryNameTaken`.
- `.mockery.yaml` — add `RecipeCategoryRepository`; re-run `mockery`.

## Acceptance criteria

- [ ] `middleware.RequireRole("ADMIN")` gates `POST`/`PATCH`/`DELETE`;
      non-ADMIN → 403 `forbidden`; no token → 401.
- [ ] `GET /recipe-categories` needs no token, returns all rows ordered
      by `name`, shape `[{id, name, createdAt}]`.
- [ ] `POST` with a valid ADMIN token + `{name}` → 201 + the bare
      resource.
- [ ] `POST` / `PATCH` duplicate `name` → 409 `name_taken`.
- [ ] `POST` / `PATCH` missing/blank `name` → 400 `name_required`; over
      length → `name_too_long`.
- [ ] `PATCH` unknown id → 404 `not_found`.
- [ ] `DELETE` unknown id → 404 `not_found`; `DELETE` a category still
      tagged on a recipe → 409 `category_in_use`; `DELETE` an unused
      category → 204.
- [x] `000005` migration is reversible (`migrate down` one step restores
      CASCADE) — confirmed live against Neon (`confdeltype` r→c). RESTRICT
      raising `23001` (not `23503`) still needs confirming from the
      repository layer's actual `DELETE` in step 2 — the migration-level
      check above only confirmed the constraint mode, not the SQLSTATE a
      live delete-in-use raises through this specific table.
- [x] `000006` seed is idempotent (`migrate up` a second time was a
      `no change`) and matches the MongoDB `RecipeCategory.name` values
      (all 28, verified against the export).
- [ ] `requests/recipe-categories.http` added.
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Non-goals

- Recipe creation/update/search and the recipe↔category association
  itself — CROC-014/015/016.
- Any admin UI — `master-spec.md:62-63` non-goal, same as CROC-010.
- `recipeUsageCount` in the list response, and a `GET /:id` route — add
  when a real consumer exists (same deferral as CROC-010's
  `itemUsageCount`).
- Reconsidering `item_allowed_units`'s CASCADE — unrelated, already
  settled at CROC-010/011.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| `RequireRole` | Already built (CROC-010), reused unchanged | n/a |
| Repository | Service boundary, test-first, real Neon dev DB | `./scripts/test-repo.sh -run TestRecipeCategory` — create; list ordering; `23505` name → domain error; update; delete; `23001` in-use → domain error; delete-missing → not-found |
| Handler | Logic, test-first, mocked repo | `go test ./internal/handler/...` — list; create validation/conflict/success; patch not-found/conflict/success; delete not-found/in-use/success |
| Full wiring | Manual `.http` regression | `requests/recipe-categories.http` top-to-bottom in VS Code REST Client: public GET (no token), ADMIN CRUD happy path, the 409/404 translations, in-use delete against a real tagged recipe once CROC-014 exists (until then, exercise in-use via a manually inserted `recipe_categories_recipes` row), Cleanup |
| Migrations | Manual | `migrate up` then `migrate down` one step for `000005` and `000006`; confirm reversibility, confirm `\d recipe_categories_recipes` shows RESTRICT, confirm idempotent re-seed |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt` |
| Review | Gate | `/code-review` (CodeRabbit trial ended mid-CROC-010, per `LESSONS.md`), fix in one pass, then close-out |

## Piece order (AI-driven)

1. **Migrations `000005` + `000006`** — done, see "Step 1 status" above.
   Still open: `internal/sqlc/queries/recipe_categories.sql` +
   `sqlc generate` + `models/recipe_category.go` + `models/errors.go`
   entries + `toModelRecipeCategory`. Mechanical; generated code isn't
   TDD-stubbed — the repo tests in step 2 cover it.
2. **`repository/recipe_category.go`** — real-DB repo tests, stub with
   fake sentinels, red → stop → green → stop.
3. **`handler/recipe_category_handler.go`** +
   `RecipeCategoryRepository` interface — mocked-repo handler tests,
   stub, red → stop → green → stop. Re-run `mockery`.
4. **Wire `main.go`** (public `GET` on `server`; the existing
   ADMIN-gated group for the writes) + `requests/recipe-categories.http`.
   Manual `.http` + migration up/down verification.

## Founder action — MongoDB export (done)

Founder provided the `RecipeCategory` collection export
(`crockpotV3.RecipeCategory.json`, 28 documents). All 28 `name` values
checked clean — no blank/malformed rows (per CROC-011's lesson) — and
turned into the `000006` seed `INSERT` verbatim, Mongo export order.

## Step 1 status — done and verified live

- Confirmed the FK's actual constraint name against the real dev DB
  before writing `000005`, rather than assuming it (per
  `~/.claude/CLAUDE.md` rule 2): `pg_constraint` on
  `recipe_categories_recipes` showed
  `recipe_categories_recipes_category_id_fkey`, `confdeltype = 'c'`
  (CASCADE) — matched what the migration assumed.
- `migrate up` applied `000005` + `000006` cleanly against Neon dev.
  Re-verified via `pg_constraint`: `confdeltype` now `'r'` (RESTRICT);
  `recipe_categories` has all 28 seeded rows.
- `migrate up` a second time: `no change` (idempotent at head).
- `migrate down 2`: both migrations reverse cleanly —
  `confdeltype` back to `'c'`, `recipe_categories` back to 0 rows.
  Reapplied `migrate up` to leave dev DB at head afterward.
- Acceptance criteria's `000005`/`000006` reversibility + idempotency
  checkboxes are satisfied by this run.

## Master-spec changes made alongside this handoff

- None required — the "reference-data reads are public, writes are
  ADMIN-gated" architecture note already covers `recipe_categories`
  explicitly (`master-spec.md:200`). This ticket's one schema decision
  (CASCADE→RESTRICT) is local to `recipe_categories_recipes` and doesn't
  change any documented architecture, so it's recorded here only, not
  duplicated into the spec.

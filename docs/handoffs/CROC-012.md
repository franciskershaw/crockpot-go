# CROC-012 — Items CRUD, with allowed-units association

**Implementation mode: AI-driven.** Same cadence as `CROC-010`/`CROC-011`:
failing test → minimal stub that fails for the right reason (fake
sentinel values, never a panic) → confirm red → stop for go-ahead →
implement → confirm green → stop before the next piece.

## Summary

`/items` — the third reference-data resource, and the first many-to-many
join table (`item_allowed_units`) in the codebase. Public `GET`,
ADMIN-only writes, reusing `middleware.RequireRole` and the
`handler/{errors,validation}.go` helper layer unchanged. New territory:
managing `item_allowed_units` as part of the item write, and the first
non-`AuthHandler` consumer of `handler.Transactor`. Classified
expensive-to-undo at grill time — the join-table update pattern and
transaction-boundary choice here are likely the template for future
many-to-many relations (`recipe_categories_recipes` at minimum).

## Decisions from the interview

### 1. `allowedUnitIds` — IDs only, not hydrated unit objects

Response shape: `{id, name, categoryId, allowedUnitIds: [uuid, ...],
createdAt}`. Rejected: hydrating each id into `{id, name, abbreviation}`
(what the old app's Prisma `include` did).

- No consumer needs unit names inline today — `crockpot-react` has no
  admin UI (unchanged since `CROC-010`'s grill). A future consumer that
  needs unit details already has the full list from `GET /units` and can
  join client-side.
- Explicitly *not* a "no admin UI ever" bet — the founder confirmed one
  is planned eventually. The reason this is still safe to defer: going
  from ids-only to hydrated later is a pure additive response-shape
  change (a repository query change + a response field), no schema
  migration, nothing else depends on the shape staying fixed. The parts
  of this ticket that *are* genuinely hard to undo (decisions 2-3, 5-6
  below) don't interact with this one.

### 2. `PATCH` replaces the whole `allowedUnitIds` set — no incremental endpoints

No `POST /items/:id/allowed-units` / `DELETE
/items/:id/allowed-units/:unitId`. `PATCH /items/:id` with
`allowedUnitIds` present deletes all existing `item_allowed_units` rows
for that item and inserts the submitted set, in one transaction (see
decision 5).

- Matches the old app's actual behavior exactly: `allowedUnitIds` there
  is a plain Mongo array field, so `updateItem.ts`'s Prisma field update
  already means "whatever you send becomes the new array" — not a new
  design choice, a direct port.
- A multi-select-style admin UI naturally produces "here's the full new
  set" on every save, not incremental toggles.

### 3. Bad `category_id`/`unit_id` — catch the real FK violation, no pre-check

`POST`/`PATCH` with a nonexistent `category_id` or `unit_id` fails
Postgres's FK constraint on insert (`23503`,
`foreign_key_violation` — the genuine insert-side code, distinct from
`23001`'s delete-side `RESTRICT` case used elsewhere in this codebase) →
`400 {"error": "invalid_category_id"}` / `400 {"error":
"invalid_unit_id"}`. No pre-check `SELECT` against `item_categories`/
`units` first.

- Same "DB is the source of truth" principle as `CROC-010` decision 4 —
  a pre-check has the same TOCTOU gap, and inspecting the real error is
  free once already catching `pgconn.PgError`.
- A malformed (non-UUID-shaped) string anywhere an id is expected
  (`category_id`, or any entry in `allowedUnitIds`) fails before it
  reaches the DB — collapses to the generic `400 invalid_request` (can't
  bind as a UUID param), not a specific `invalid_*_id` code. That's "not
  a well-formed request," a different state from "well-formed id that
  doesn't exist."
- One shared `handler` helper parses a request-supplied id string into a
  UUID, used for both `category_id` and each `allowedUnitIds` entry —
  not two near-identical validators.

### 4. No seed migration — real item data deferred to `CROC-024`

Unlike `item_categories` (13 rows) and `units` (20 rows), `CROC-012`
ships the `/items` CRUD API with an empty table — no hand-written seed
from the old app's Mongo `Item` collection.

- Items are a much larger, open-ended catalog (dozens-to-hundreds of
  ingredients) than the two small closed reference sets already seeded.
  Seeding correctly means resolving each item's Mongo `categoryId`/
  `allowedUnitIds` (ObjectIds) to the already-seeded Postgres
  `item_categories`/`units` UUIDs — real cross-collection ID-remapping
  logic.
- `CROC-024`'s `cmd/migrate-data` epic already exists to solve exactly
  this: reads via the repository layer (same create/validation logic as
  any API-created row), rerunnable against a wiped dev DB, one real run
  at cutover. A hand-written seed here would duplicate that mapping
  logic ahead of time, worse.
- Consequence: `/items` is empty locally until `CROC-024` runs. Nothing
  in the current backlog needs real item data before then.

### 5. `ItemHandler` takes a `Transactor` — first non-auth consumer

`ItemHandler.Create`/`Update` wrap their repository call in
`h.transactor.WithinTx(ctx, func(ctx) error { ... })`, same as
`AuthHandler.ResetPassword` already does for its own multi-repo write.
`PostgresItemRepository.Create`/`Update` do their multi-statement work
(the `items` row, then the `item_allowed_units` rows) as plain
sequential `queriesFor(ctx, r.db)` calls — no repository-internal
`Begin`/`Commit`, no new abstraction. `main.go` passes the
already-constructed `transactor` into `NewItemHandler` too.

- Reuses the established, tested pattern exactly (`queriesFor` picks up
  the tx from context automatically) rather than inventing a
  repository-internal transaction mechanism, which would require
  `ItemRepository` to hold the raw pool instead of the `sqlc.DBTX`
  interface every repository built so far uses — breaking testability
  against a transaction context and diverging from precedent.
- `Delete` needs no transaction: `item_allowed_units.item_id` is `ON
  DELETE CASCADE`, so deleting the `items` row alone correctly removes
  its own join rows in the same statement's cascade — a single-statement
  operation, matching `item_categories`/`units`'s `Delete`.

### 6. Partial-update sentinel for `category_id` — `sqlc.narg`, not the `NULLIF('', ...)` trick

`name` reuses the existing `COALESCE(NULLIF(sqlc.arg(name)::text, ''),
name)` pattern. `category_id` is a `UUID` column with no meaningful
"empty" sentinel, so its partial-update uses sqlc's nullable-arg form
instead: `COALESCE(sqlc.narg(category_id), category_id)`. The handler
passes a real SQL `NULL` when `category_id` wasn't in the `PATCH` body,
a real UUID when it was.

- Works cleanly specifically because `category_id` is genuinely never
  `NULL` in the schema (`NOT NULL`) — `NULL` is unambiguous as "no
  update requested," no collision risk the way an empty string could
  theoretically collide with a rejected-by-validation real value on a
  `TEXT` column.
- First partial-update on a non-`TEXT` column in this codebase — the
  pattern for any future non-`TEXT` optional `PATCH` field.
- `allowedUnitIds` follows the same optional-field shape `name`/`icon`
  already use in request structs: `*[]string` in JSON binding (nil =
  omitted vs. present-and-empty = explicit clear), translated to a plain
  `[]string` at the repository boundary (`nil` = don't touch,
  non-nil — including empty — = replace with this set), matching Go's
  native nil-vs-empty-slice distinction. No extra bool flag needed.

### 7. Endpoint surface

| Method | Path | Auth | Success | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/items` | public | 200 | `[{id, name, categoryId, allowedUnitIds, createdAt}]`, ordered by `name` |
| `POST` | `/items` | ADMIN | 201 | the created resource, bare |
| `PATCH` | `/items/:id` | ADMIN | 200 | the updated resource, bare |
| `DELETE` | `/items/:id` | ADMIN | 204 | none |

Same shape rules as `CROC-010`/`CROC-011`: no `GET /:id`, no usage-count
field (no admin UI consumer yet — old app's `recipeCount` on `getItems`
isn't ported), bare resource on create/update. `category_id` required on
`POST` (`400 category_id_required` if missing/blank); `allowedUnitIds`
optional on `POST` (defaults to empty — "use category defaults", per the
old app's own field comment). `PATCH` partial (`{name?, categoryId?,
allowedUnitIds?}`, at least one present or `400 invalid_request`).

## Non-goals

- Hydrated `allowedUnits` (full unit objects) in the response — decision
  1, add when a real consumer needs it.
- Incremental add/remove-unit endpoints — decision 2, full-replace only.
- Real item data — decision 4, `CROC-024`'s job.
- The old app's `WATER_ITEM_ID`/`HOUSE_CATEGORY_ID`-style
  ingredient-picker filtering (`crockpot/src/data/items/getItems.ts`) —
  consumption-side UX for recipe creation, not admin CRUD. `CROC-014`'s
  concern if it turns out to be needed, not assumed here.
- Any admin UI — still API-only (a real future plan, not this ticket).
- `GET /items/:id` — add when a real single-fetch consumer appears, same
  precedent as `item_categories`/`units`.

## Schema + data changes

No migration needed — `items` and `item_allowed_units` already exist
unmodified from `CROC-002`'s base schema (`000001_init.up.sql:83-96`).

- `internal/sqlc/queries/items.sql`:
  - `ListItems :many` — `items` only, ordered by `name`.
  - `ListItemAllowedUnitIDsForItems :many` — `SELECT item_id, unit_id
    FROM item_allowed_units WHERE item_id = ANY(sqlc.arg(item_ids)::uuid[])
    ORDER BY unit_id` — one query for the whole list's allowed-units,
    grouped by `item_id` in Go. Avoids both N+1 (one query per item) and
    the `array_agg`-over-`LEFT JOIN`-with-no-rows `NULL`-element gotcha a
    single aggregated query would need extra handling for.
  - `CreateItem :one` — insert into `items`, return the row.
  - `CreateItemAllowedUnit :exec` — insert one `(item_id, unit_id)` row;
    called in a loop from the repository (small counts, no bulk-insert
    complexity needed for v1).
  - `DeleteItemAllowedUnitsForItem :exec` — delete all
    `item_allowed_units` rows for an item (used by `Update`'s
    full-replace; not needed by `Delete`, which relies on `CASCADE`).
  - `ListItemAllowedUnitIDs :many` — `unit_id`s for one item, ordered,
    for `Create`'s/`Update`'s own return value.
  - `UpdateItem :one` — partial via `COALESCE(NULLIF(...))` for `name`,
    `COALESCE(sqlc.narg(category_id), category_id)` for `category_id`.
  - `DeleteItem :one` — `RETURNING id`.
- Regenerate: `sqlc generate`.
- `internal/models/item.go` — `Item{ID uuid.UUID, Name string,
  CategoryID uuid.UUID, AllowedUnitIDs []uuid.UUID, CreatedAt
  time.Time}` (`UpdatedAt` unexposed, matching `ItemCategory`/`Unit`).
- `internal/models/errors.go` — `ErrItemNotFound`, `ErrItemInUse`,
  `ErrItemNameTaken`, `ErrItemInvalidCategory`, `ErrItemInvalidUnit`.
- `.mockery.yaml` — add `ItemRepository`; re-run `mockery`.

## Acceptance criteria

- [ ] `middleware.RequireRole("ADMIN")` gates `POST`/`PATCH`/`DELETE`
      (reused unchanged); non-ADMIN → 403 `forbidden`; no token → 401.
- [ ] `GET /items` needs no token, returns all rows ordered by `name`,
      shape `[{id, name, categoryId, allowedUnitIds, createdAt}]`, each
      item's `allowedUnitIds` correctly grouped (not cross-contaminated
      between items).
- [ ] `POST` with a valid ADMIN token + `{name, categoryId}` (no
      `allowedUnitIds`) → 201, `allowedUnitIds: []`.
- [ ] `POST` with `{name, categoryId, allowedUnitIds: [id1, id2]}` → 201,
      both rows present in `item_allowed_units` and in the response.
- [ ] `POST` / `PATCH` duplicate `name` → 409 `name_taken`.
- [ ] `POST` / `PATCH` nonexistent `categoryId` → 400
      `invalid_category_id`; nonexistent entry in `allowedUnitIds` → 400
      `invalid_unit_id`; malformed (non-UUID) `categoryId` or
      `allowedUnitIds` entry → 400 `invalid_request`.
- [ ] `POST` missing/blank `name` → 400 `name_required`; over 100 chars →
      `name_too_long`; missing/blank `categoryId` → 400
      `category_id_required`.
- [ ] `PATCH` with only `allowedUnitIds` replaces the full set
      (adds new, removes omitted) and leaves `name`/`categoryId`
      unchanged; `PATCH` with `allowedUnitIds: []` clears all allowed
      units; `PATCH` omitting `allowedUnitIds` entirely leaves the
      existing set untouched.
- [ ] `PATCH` unknown id → 404 `not_found`; `PATCH` with no fields
      present → 400 `invalid_request`.
- [ ] `DELETE` unknown id → 404 `not_found`; `DELETE` an item referenced
      by `recipe_ingredients` or `shopping_list_items` → 409
      `item_in_use`; `DELETE` an unused item → 204, and its
      `item_allowed_units` rows are gone too (via `CASCADE`).
- [ ] A failed `Create`/`Update` (e.g. bad `unit_id` partway through
      inserting `allowedUnitIds`) leaves no partial `items` row behind —
      proves the `Transactor` wrapping actually works, not just compiles.
- [ ] `requests/items.http` added, following `item-categories.http`'s
      shape.
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| Repository | Service boundary, test-first, real Neon dev DB | `./scripts/test-repo.sh -run TestItem` — create (with/without allowed-units); list incl. grouping; `23505` name → domain error; `23503` bad category/unit → domain errors; update (partial name/category/units incl. `sqlc.narg`); delete; `23001` in-use (either referencing table) → domain error; delete-missing → not-found; rollback-on-partial-failure |
| Handler | Logic, test-first, mocked repo (incl. mocked `Transactor`) | `go test ./internal/handler/...` — list; create validation/conflict/success; patch not-found/conflict/partial/success; delete not-found/in-use/success |
| Full wiring | Manual `.http` regression, by the founder | `requests/items.http` top-to-bottom in VS Code REST Client: public GET (no token), ADMIN CRUD happy path incl. allowed-units, the 409/404/400 translations, Cleanup |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt` |
| Review | Gate | `/code-review medium main` |

## Piece order (AI-driven)

1. **`internal/sqlc/queries/items.sql`** + `sqlc generate` +
   `models/item.go` + `models/errors.go` entries + `toModelItem`.
   Mechanical; generated code isn't TDD-stubbed — the repo tests in step
   2 cover it. Constraint names (`items_name_key`,
   `items_category_id_fkey`, `item_allowed_units_unit_id_fkey`, the
   `recipe_ingredients`/`shopping_list_items` FK names for the in-use
   check) get confirmed against the real DB during step 2, same as every
   prior ticket.
2. **`repository/item.go`** — real-DB repo tests, stub with fake
   sentinels, red → stop → green → stop.
3. **`handler/item_handler.go`** + `handler/validation.go`'s shared
   UUID-parse helper + `ItemRepository` interface — mocked-repo handler
   tests (mocking `ItemRepository` and `Transactor` both), stub, red →
   stop → green → stop. Re-run `mockery`.
4. **Wire `main.go`** (public `GET` on `server`; an ADMIN-gated group for
   the writes, passing the existing `transactor` into `NewItemHandler`
   too) + `requests/items.http`. Manual `.http` verification (founder).

Completed 2026-08-30.

# CROC-010 — Item categories CRUD

**Implementation mode: AI-driven.** Claude implements one piece at a time
per `crockpot-go/CLAUDE.md`'s AI-driven cadence: failing test → minimal
stub that fails for the right reason (fake sentinel values, never a
panic) → confirm red → stop for go-ahead → implement → confirm green →
stop before the next piece.

## Summary

`/item-categories` — the first reference-data resource. `GET` is public
(unauthenticated); `POST`/`PATCH`/`DELETE` are ADMIN-only. Reference data
is system-owned and admin-curated — no per-user rows, no ownership
column (`master-spec.md` "Ownership model").

This is the first resource handler in the repo, so it also lands the
shared pieces every Epic 3–6 ticket after it reuses:

- `middleware.RequireRole(roles ...string)` — the first role gate. Nothing
  currently enforces the `role` that `AuthMiddleware` puts in the Gin
  context (`internal/middleware/auth.go:34`).
- `internal/handler/errors.go` + `internal/handler/validation.go` — the
  first extraction of the `bindJSON` / `internalError` / `validate*`
  helpers that `auth_handler.go` currently inlines ~20×.
- The first sqlc query file for a plain resource table, the first
  mocked-repo CRUD handler test, the first `.http` file for an
  admin-gated resource.

It also renames the misnamed `item_categories.fa_icon` column to `icon`
and seeds the real category list from the old app's MongoDB (see
"Schema + data changes" and "Founder action" below).

## Decisions from the interview

### 1. Admin gate — a `RequireRole` middleware, built here

`middleware.RequireRole(roles ...string) gin.HandlerFunc` — reads the
`role` string `AuthMiddleware` already sets, calls `c.Next()` if it
matches one of `roles`, otherwise `403 {"error": "forbidden"}` and
abort. Applied at the router-group level to the write routes, *after*
`AuthMiddleware` in the same group.

- Variadic so `RequireRole("ADMIN")` reads clean now and a multi-role
  gate needs no signature change later.
- The check is genuinely just "is the caller ADMIN" — no per-record
  ownership, no tier ordering — so a route gate expresses it more
  honestly than a per-handler helper and keeps auth branching out of the
  handler methods and their unit tests.
- Rejected: pulling CROC-023's full tier-gate helper forward. That
  ticket's helper is for the data-dependent "FREE user hit their
  5-recipe cap → 403 with an upgrade message" check (CROC-014) — a
  different shape from a static route gate. Traced every CROC-023
  consumer: CROC-011/012/013/017 all need exactly this route gate;
  only CROC-025 (planner, `role ≥ PREMIUM`, Epic 9) needs tier
  *ordering*, and it can pass an explicit role set or add an ordering
  helper when it lands. Nothing between here and Epic 4 is blocked.
- Rejected: inlining the role check per write handler — it's the thing
  we'd factor out by CROC-012 anyway.

**Consequence — no ADMIN user exists in the dev DB.** Testing the write
endpoints (and the `.http` login chain) needs a one-time
`UPDATE users SET role = 'ADMIN' WHERE email = '<the shared test
account>'` in the Neon dev DB. Documented in the `.http` file header and
`requests/README.md`. Harmless to `auth.http` / `me.http` — neither
asserts on role.

### 2. `GET /item-categories` is public — no `AuthMiddleware`

Registered directly on `server`, like `/health`, before any authed
group.

- The spec's "public reads" sits opposite "admin-only writes"; the
  ownership bullet says reference data is "visible to everyone" — the
  natural reading is unauthenticated.
- Anonymous users browse and filter recipes (by category, by
  ingredient) without logging in — the API should treat reference-data
  reads the same way. This generalises: `units` (CROC-011) and
  `recipe_categories` (CROC-013) are public reads for the same reason.
  Recorded as an architecture note in `master-spec.md`.
- Zero sensitivity — a curated list of grocery-aisle names and icon
  tokens.
- Reversible without a contract change: per-IP rate limiting or auditing
  later is a middleware add; `GET` stays `GET`.

### 3. Endpoint surface

| Method | Path | Auth | Success | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/item-categories` | public | 200 | `[{id, name, icon, createdAt}]`, ordered by `name` |
| `POST` | `/item-categories` | ADMIN | 201 | the created resource, bare |
| `PATCH` | `/item-categories/:id` | ADMIN | 200 | the updated resource, bare |
| `DELETE` | `/item-categories/:id` | ADMIN | 204 | none |

- **No `GET /item-categories/:id` route.** Every consumer needs the whole
  list (~tens of rows); `PATCH`/`DELETE` get their existence check from
  `RETURNING` (decision 5). Add the route when a real single-fetch
  consumer appears.
- **No `itemUsageCount` in the list payload.** The old app returned it
  for its admin table; crockpot-react has no admin category UI
  (`master-spec.md` non-goal), so nothing consumes it. `DELETE` still
  guards on usage — it just isn't exposed. Add it to the payload when
  the admin-UI ticket needs it.
- **`PATCH`, partial** — `{name?, icon?}`, at least one present or
  `400 {"error": "invalid_request"}`.
- **Bare resource** on create/update, not `{data: …}`-wrapped — matches
  `packing-list-go` and `master-spec.md`'s success-shape rule.
- JSON field is `icon` (see decision 8's rename), not `faIcon`.

### 4. Conflict handling — catch `23505`, distinct codes

`item_categories` has two independent unique constraints. The repository
catches the `pgconn.PgError` `23505` and branches on `ConstraintName`;
**no pre-check `SELECT`**.

- `item_categories_name_key` → `409 {"error": "name_taken"}`
- `item_categories_icon_key` → `409 {"error": "icon_taken"}`

Reasoning:
- Matches the established pattern in `internal/repository/user.go`
  (the only repo precedent) — it's the house style.
- Race-free: a pre-check + insert has a TOCTOU window, the exact class
  behind CROC-008's lesson and repeated CodeRabbit catches. The unique
  index is the source of truth.
- Distinct codes are nearly free once you're inspecting `ConstraintName`
  and genuinely help an admin filling the form. The spec's "collapse to
  generic `invalid_request`" rule is about malformed-JSON validation,
  not semantically distinct states (cf. `email_registered_with_google`
  vs `..._with_password`).
- On `PATCH`, setting a field to its current value is a no-op `UPDATE`
  that doesn't trip the constraint — no `excludeID` dance needed.

**Fragility to watch:** the repo maps Postgres's auto-generated
constraint names. A future migration that renames a constraint silently
drops the mapping to a generic 500. The `000002` migration renames the
`fa_icon` constraint deliberately (decision 8) and the code is updated in
lockstep; any later rename must do the same.

### 5. Not-found + delete-in-use — `RETURNING`, catch `23503`

No separate `GetByID` fetch.

- `DELETE FROM item_categories WHERE id = $1 RETURNING id` (`:one`)
  - `pgx.ErrNoRows` → `models.ErrItemCategoryNotFound` →
    `404 {"error": "not_found"}`
  - `23503` foreign-key violation (an `items` row references it; the FK
    is `ON DELETE RESTRICT`, so it fires on the statement) →
    `models.ErrItemCategoryInUse` → `409 {"error": "category_in_use"}`
- `UPDATE item_categories SET … WHERE id = $1 RETURNING *` (`:one`)
  - `ErrNoRows` → 404
  - `23505` → 409 per decision 4

Reasoning: consistent with decision 4 (DB is the source of truth), one
round-trip, race-free. A `COUNT` + `DELETE` has the same TOCTOU window as
a pre-check insert. The 409 can't report *how many* items block the
delete — nothing consumes that today; add a friendly `COUNT` on top of
the FK backstop if an admin UI ever wants it.

### 6. Validation + the handler helper layer

**Start `internal/handler/errors.go` and `internal/handler/validation.go`
in this ticket.**

- `internalError(c, logMsg string, err error)` — `_ = c.Error(fmt.Errorf(
  "%s: %w", logMsg, err))` then `c.JSON(500, {"error": "server_error"})`.
  Exactly the pattern `auth_handler.go` inlines ~20×; the `c.Error` is
  surfaced by `gin.Default()`'s logger, so no `ErrorLogger` middleware is
  needed. Shape ported from `packing-list-go/internal/handler/errors.go`,
  client code fixed to `server_error`.
- `bindJSON(c, target any) bool` → `400 {"error": "invalid_request"}` on
  failure.
- `validateName(c, raw) (string, bool)` → trim; empty →
  `name_required`; `len > 100` → `name_too_long`.
- `validateIconToken(c, raw) (string, bool)` → trim; empty →
  `icon_required`; `len > 64` → `icon_too_long`.

Specific codes for semantic validation match `auth_handler.go`'s
`password_too_short` / `password_too_long`; `invalid_request` stays
reserved for bind failures.

- **No server-side allowlist for `icon`** — a maintained FontAwesome/
  lucide name list would drift from whatever icon-library version
  crockpot-react ships; the admin is trusted and the frontend form
  constrains the choice.
- `100` / `64` are sane guardrails, not real constraints (column is
  `TEXT`).
- **Not** retrofitting `auth_handler.go` onto these helpers here —
  trivial follow-up, out of scope, flagged.

### 7. Implementation mode — AI-driven

Hand-written mode is for building familiarity with new stack territory;
there is none here — `RequireRole` is standard Gin middleware and the
sqlc → generated model → repository → mocked-handler-test chain is what
the auth epic already built five times. It's the template for ~10 more
tickets, which argues for getting it reviewed (PR + CodeRabbit), not for
doing it slowly by hand.

### 8. Seed the real categories + rename `fa_icon` → `icon`

The old app's `faIcon` field stores **lucide-react component names**
(`"Package"`, `"Carrot"`, PascalCase — `crockpot/src/lib/icon-map.tsx`,
`src/components/dialogs/ItemCategoryDialog.tsx:44`), not FontAwesome
names, despite the column name. crockpot-react also uses lucide, so those
values are correct for the new frontend; the column name is just
legacy-misleading and nothing depends on it yet.

- **`000002_rename_item_category_icon`** — `ALTER TABLE item_categories
  RENAME COLUMN fa_icon TO icon;` plus `ALTER TABLE item_categories
  RENAME CONSTRAINT item_categories_fa_icon_key TO
  item_categories_icon_key;` (a column rename does *not* rename its
  constraint automatically). Down reverses both.
- **`000003_seed_item_categories`** — `INSERT INTO item_categories
  (name, icon) VALUES … ON CONFLICT (name) DO NOTHING;` from the founder's
  MongoDB export (see "Founder action"). Down deletes exactly those
  `name`s.
- All request/response fields, error codes, validators, sqlc columns and
  models use `icon` from the outset — there is no `fa_icon` anywhere in
  new code.
- **CROC-024 overlap** (low risk, flagged for its grill): the data
  migration is "safe to rerun against a wiped dev DB" (wipes first), so
  the seed and the migration don't fight; on a prod cutover the migration
  seeds first, then CROC-024's prod run reconciles against Mongo as the
  authority.

## Schema + data changes

- `000002_rename_item_category_icon.up.sql` / `.down.sql` — column +
  constraint rename, reversible.
- `000003_seed_item_categories.up.sql` / `.down.sql` — idempotent seed
  (`ON CONFLICT (name) DO NOTHING`), down deletes the seeded `name`s.
- `internal/sqlc/queries/item_categories.sql` — `ListItemCategories`,
  `CreateItemCategory`, `UpdateItemCategory` (partial via
  `COALESCE(NULLIF(sqlc.arg(...)::text, ''), col)` per
  `queries/users.sql`'s `UpdateUserLoginProfile`), `DeleteItemCategory`
  (`… RETURNING id`).
- Regenerate: `sqlc generate` (adds `ItemCategory` to
  `internal/sqlc/models.go` + `item_categories.sql.go`).
- `internal/models/item_category.go` — `ItemCategory{ ID uuid.UUID,
  Name string, Icon string, CreatedAt time.Time }` (`UpdatedAt` unexposed,
  matching `models.User`).
- `internal/models/errors.go` — `ErrItemCategoryNotFound`,
  `ErrItemCategoryInUse`, `ErrItemCategoryNameTaken`,
  `ErrItemCategoryIconTaken`.
- `.mockery.yaml` — add `ItemCategoryRepository`; re-run `mockery`.

## Acceptance criteria

- [ ] `middleware.RequireRole("ADMIN")` gates `POST`/`PATCH`/`DELETE`;
      non-ADMIN (FREE/PREMIUM/PRO) → 403 `forbidden`; no token → 401 from
      `AuthMiddleware`.
- [ ] `GET /item-categories` needs no token, returns all rows ordered by
      `name`, shape `[{id, name, icon, createdAt}]`.
- [ ] `POST` with a valid ADMIN token + `{name, icon}` → 201 + the bare
      resource.
- [ ] `POST` / `PATCH` duplicate `name` → 409 `name_taken`; duplicate
      `icon` → 409 `icon_taken`.
- [ ] `POST` / `PATCH` missing/blank `name` → 400 `name_required`; over
      length → `name_too_long`; same for `icon`.
- [ ] `PATCH` unknown id → 404 `not_found`; `PATCH` with neither field →
      400 `invalid_request`; `PATCH` one field leaves the other
      unchanged.
- [ ] `DELETE` unknown id → 404 `not_found`; `DELETE` a category with an
      `items` row → 409 `category_in_use`; `DELETE` an unused category →
      204.
- [ ] `000002` rename is reversible; after it, no `fa_icon` remains in
      schema or code.
- [ ] `000003` seed is idempotent (second run is a no-op) and matches the
      MongoDB `name`/`icon` values.
- [ ] `requests/item-categories.http` added; `requests/README.md` notes
      the one-time ADMIN bump.
- [ ] `auth_handler.go` still green — helper extraction didn't change its
      behaviour.
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Non-goals

- `units`, `items`, `recipe_categories` CRUD — CROC-011/012/013.
- Any admin UI — crockpot-react has none for reference data
  (`master-spec.md` non-goal); this is API-only.
- `itemUsageCount` in the list response, and a `GET /:id` route — add
  when a real consumer exists.
- Retrofitting `auth_handler.go` onto the new helper files.
- CROC-023's tier-ordering / recipe-cap helper — reduced to CROC-014's
  dependency (spec updated).
- Generalising `RequireRole` to a `≥`-ordering gate — CROC-025's problem.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| `RequireRole` | Logic, test-first | `go test ./internal/middleware/...` — table over ADMIN→next, FREE/PREMIUM/PRO/missing→403 |
| Repository | Service boundary, test-first, real Neon dev DB | `./scripts/test-repo.sh -run TestItemCategory` — create; list ordering; `23505` name/icon → domain errors; update (incl. partial); delete; `23503` in-use → domain error; delete-missing → not-found |
| Handler | Logic, test-first, mocked repo | `go test ./internal/handler/...` — list; create validation/conflict/success; patch not-found/conflict/partial/success; delete not-found/in-use/success |
| Full wiring | Manual `.http` regression | `requests/item-categories.http` top-to-bottom in VS Code REST Client: public GET (no token), ADMIN CRUD happy path, the 409/404 translations, Cleanup |
| Migrations | Manual | `migrate up` then `migrate down` one step for `000002` and `000003`; confirm reversibility and idempotent re-seed |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt` |
| Review | Gate | PR → CodeRabbit, fix in one pass, then close-out |

## Piece order (AI-driven)

1. **`middleware.RequireRole`** — table-driven test, stub, red → stop →
   green → stop.
2. **Migrations `000002` + `000003`** (`000003` with a placeholder single
   row until the founder's export lands) + `internal/sqlc/queries/
   item_categories.sql` + `sqlc generate` + `models/item_category.go` +
   `models/errors.go` entries + `toModelItemCategory`. Mechanical;
   generated code isn't TDD-stubbed — the repo tests in step 3 cover it.
3. **`repository/item_category.go`** — real-DB repo tests, stub with fake
   sentinels, red → stop → green → stop.
4. **`handler/item_category_handler.go`** + `handler/errors.go` +
   `handler/validation.go` + `ItemCategoryRepository` interface —
   mocked-repo handler tests, stub, red → stop → green → stop. Re-run
   `mockery`.
5. **Wire `main.go`** (public `GET` on `server`; an ADMIN-gated group for
   the writes) + `requests/item-categories.http` + `requests/README.md`
   note. Manual `.http` + migration up/down verification.

## Founder action — MongoDB export (before step 2 completes)

Claude will **stop and ask** for this. From MongoDB Compass, connected to
the production DB:

- **Confirm the collection name** in the sidebar — Prisma maps the
  `ItemCategory` model to a collection of the same name (no `@@map` in
  `crockpot/prisma/schema.prisma`), so it should be `ItemCategory`, but
  check.
- **Option A (mongosh tab, bottom of Compass):**
  ```
  db.ItemCategory.find({}, { name: 1, icon: "$faIcon", _id: 0 }).toArray()
  ```
  or simply `db.ItemCategory.find({}, { name: 1, faIcon: 1, _id: 0 }).toArray()`
  and paste the array here.
- **Option B (GUI):** select the `ItemCategory` collection → **Export
  Data** → **Export the full collection** → **JSON** → export, then paste
  or attach the file.

Claude turns the `{name, faIcon}` pairs into the `000003` seed `INSERT`
(field renamed to `icon` in the SQL).

## Master-spec changes made alongside this handoff

- Epic 7 / CROC-023 — scope reduced to the recipe-cap limit helper
  (CROC-014's dependency); route-level role gating moves to
  `RequireRole`, landed in CROC-010.
- "Key architecture decisions" — new bullet: reference-data reads
  (`item_categories`, `units`, `recipe_categories`) are public /
  unauthenticated, to serve anonymous recipe browse/filter; writes are
  ADMIN-gated via `middleware.RequireRole`.
- Non-goal wording about the "Pricing page placeholder" is untouched
  (that's a crockpot-react concern).

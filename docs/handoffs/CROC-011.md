# CROC-011 — Units CRUD

**Implementation mode: AI-driven.** Same cadence as `CROC-010`: failing
test → minimal stub that fails for the right reason (fake sentinel
values, never a panic) → confirm red → stop for go-ahead → implement →
confirm green → stop before the next piece.

## Summary

`/units` — the second reference-data resource, built directly on
`CROC-010`'s already-decided architecture (public `GET`, ADMIN-only
writes via `middleware.RequireRole`, the `handler/{errors,validation}.go`
helper layer, the response-shape/conflict-handling conventions). No new
shared infrastructure lands here — pure reuse of CROC-010's template
applied to a second table. Classified cheap-to-undo at grill time; this
handoff doc exists anyway per this project's own convention (one per
ticket, `CLAUDE.md`'s Docs section), not because the decisions below are
architecturally expensive.

## Decisions from the interview

### 1. Delete-in-use — catch `23001` generically, no `ConstraintName` split

Unlike `item_categories` (one referencing table), `units` has three:
`item_allowed_units.unit_id` (`ON DELETE CASCADE`),
`recipe_ingredients.unit_id` (nullable, `ON DELETE RESTRICT`),
`shopping_list_items.unit_id` (nullable, `ON DELETE RESTRICT`) — all from
`CROC-002`'s schema, unchanged here.

- `DELETE` catches Postgres `23001` (`restrict_violation`) from *either*
  RESTRICT-guarded table as the same `409 {"error": "unit_in_use"}`,
  without branching on `ConstraintName` — there's nothing useful to tell
  an admin about *which* table blocked it, unlike the name/abbreviation
  conflict split where the two states are genuinely different.
- `item_allowed_units`'s `ON DELETE CASCADE` is untouched — deleting a
  unit silently drops it from any item's allowed-unit list rather than
  being blocked. Pre-existing schema behavior from `CROC-002`, not
  reopened here; changing it would be its own migration + its own
  discussion (is one item still allowing a unit worth blocking a delete
  over?), and neither `item_allowed_units`-consumption (`CROC-012`) nor
  `recipe_ingredients`/`shopping_list_items` (Epics 4-6) have any live
  data yet regardless.

### 2. `abbreviation` validation — required, 32-char cap

New validator, `validateAbbreviation(c, raw) (string, bool)` in
`handler/validation.go` — trim; empty → `abbreviation_required`;
`len > 64` is `icon`'s cap on a semantically different field (a lucide
component name, can run long); an abbreviation (`g`, `kg`, `tbsp`,
`pinch`, `clove`, ...) never approaches that. `32` is the cap: generous
headroom, but a deliberately distinct number, not `icon`'s reused as-is.
Old app's `UnitDialog.tsx` enforces no length limit at all (checked, not
a precedent to follow). `name` reuses `validateName` unchanged — same
field shape, same cap, same codes as `item_categories.name`.

### 3. Seed data — real export, one row excluded

Founder exported the old app's `Unit` Mongo collection directly
(`crockpot.Unit`, 21 rows after exclusion). One row —
`{name: "", abbreviation: ""}` (`_id: 68738ad4d5730ccdb15ca141`) —
excluded from the seed: the old app's `Item`/`RecipeIngredient`/
`ShoppingListItem` models all have a nullable `unitId`, so "no unit" was
already representable as `null`, not a placeholder empty-string `Unit`
row; it's also unseedable-through-the-API, since it can't satisfy this
ticket's own `name_required`/`abbreviation_required` validation. No other
name/abbreviation collisions across the 21 remaining rows.

### 4. Endpoint surface

| Method | Path | Auth | Success | Body |
| --- | --- | --- | --- | --- |
| `GET` | `/units` | public | 200 | `[{id, name, abbreviation, createdAt}]`, ordered by `name` |
| `POST` | `/units` | ADMIN | 201 | the created resource, bare |
| `PATCH` | `/units/:id` | ADMIN | 200 | the updated resource, bare |
| `DELETE` | `/units/:id` | ADMIN | 204 | none |

Same shape rules as `CROC-010`: no `GET /:id`, no `unitUsageCount` in the
list payload (no admin UI consumer), `PATCH` partial (`{name?,
abbreviation?}`, at least one present or `400 invalid_request`), bare
resource on create/update.

## Schema + data changes

- `db/migrations/000004_seed_units.up.sql` / `.down.sql` — idempotent
  seed (`ON CONFLICT (name) DO NOTHING`) from the real export above; down
  deletes exactly those `name`s. No rename migration needed — `name`/
  `abbreviation` already match between old and new schemas.
- `internal/sqlc/queries/units.sql` — `ListUnits`, `CreateUnit`,
  `UpdateUnit` (partial via `COALESCE(NULLIF(sqlc.arg(...)::text, ''),
  col)`), `DeleteUnit` (`RETURNING id`).
- Regenerate: `sqlc generate`.
- `internal/models/unit.go` — `Unit{ID uuid.UUID, Name string,
  Abbreviation string, CreatedAt time.Time}` (`UpdatedAt` unexposed,
  matching `ItemCategory`).
- `internal/models/errors.go` — `ErrUnitNotFound`, `ErrUnitInUse`,
  `ErrUnitNameTaken`, `ErrUnitAbbreviationTaken`.
- `.mockery.yaml` — add `UnitRepository`; re-run `mockery`.

## Acceptance criteria

- [ ] `middleware.RequireRole("ADMIN")` gates `POST`/`PATCH`/`DELETE`
      (reused unchanged from `CROC-010`); non-ADMIN → 403 `forbidden`; no
      token → 401.
- [ ] `GET /units` needs no token, returns all rows ordered by `name`,
      shape `[{id, name, abbreviation, createdAt}]`.
- [ ] `POST` with a valid ADMIN token + `{name, abbreviation}` → 201 +
      the bare resource.
- [ ] `POST` / `PATCH` duplicate `name` → 409 `name_taken`; duplicate
      `abbreviation` → 409 `abbreviation_taken`.
- [ ] `POST` / `PATCH` missing/blank `name` → 400 `name_required`; over
      100 chars → `name_too_long`; missing/blank `abbreviation` → 400
      `abbreviation_required`; over 32 chars → `abbreviation_too_long`.
- [ ] `PATCH` unknown id → 404 `not_found`; `PATCH` with neither field →
      400 `invalid_request`; `PATCH` one field leaves the other
      unchanged.
- [ ] `DELETE` unknown id → 404 `not_found`; `DELETE` a unit referenced
      by `recipe_ingredients` or `shopping_list_items` → 409
      `unit_in_use`; `DELETE` an unused unit → 204.
- [ ] `000004` seed is idempotent (second run is a no-op) and matches the
      21-row MongoDB export (blank row excluded).
- [ ] `requests/units.http` added, following `item-categories.http`'s
      shape (reuses the same ADMIN test account, already documented in
      `requests/README.md`).
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean.

## Non-goals

- Any admin UI — still no consumer in `crockpot-react`.
- `unitUsageCount` in the list response, and a `GET /:id` route — same
  reasoning as `CROC-010`, add when a real consumer exists.
- Reconsidering `item_allowed_units`'s `ON DELETE CASCADE` — pre-existing
  `CROC-002` schema decision, out of scope here.
- Any change to `RequireRole` or the `handler/{errors,validation}.go`
  helper layer — pure reuse.
- `item_allowed_units` maintenance (which units an item allows) —
  `CROC-012`'s problem.

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| Repository | Service boundary, test-first, real Neon dev DB | `./scripts/test-repo.sh -run TestUnit` — create; list ordering; `23505` name/abbreviation → domain errors; update (incl. partial); delete; `23001` in-use (either referencing table) → domain error; delete-missing → not-found |
| Handler | Logic, test-first, mocked repo | `go test ./internal/handler/...` — list; create validation/conflict/success; patch not-found/conflict/partial/success; delete not-found/in-use/success |
| Full wiring | Manual `.http` regression, by the founder | `requests/units.http` top-to-bottom in VS Code REST Client: public GET (no token), ADMIN CRUD happy path, the 409/404 translations, Cleanup |
| Migrations | Manual | `migrate up` then `migrate down` one step for `000004`; confirm reversibility and idempotent re-seed |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt` |
| Review | Gate | `/code-review medium main` (per `CLAUDE.md`'s updated review process, post-CodeRabbit) |

## Piece order (AI-driven)

1. **Migration `000004`** (real seed data, no placeholder needed) +
   `internal/sqlc/queries/units.sql` + `sqlc generate` +
   `models/unit.go` + `models/errors.go` entries + `toModelUnit`.
   Mechanical; generated code isn't TDD-stubbed — the repo tests in step
   2 cover it.
2. **`repository/unit.go`** — real-DB repo tests, stub with fake
   sentinels, red → stop → green → stop.
3. **`handler/unit_handler.go`** + `handler/validation.go`'s new
   `validateAbbreviation` + `UnitRepository` interface — mocked-repo
   handler tests, stub, red → stop → green → stop. Re-run `mockery`.
4. **Wire `main.go`** (public `GET` on `server`; an ADMIN-gated group for
   the writes, same shape as `item-categories`) +
   `requests/units.http`. Manual `.http` (founder) + migration up/down
   verification.

Completed 2026-08-30.

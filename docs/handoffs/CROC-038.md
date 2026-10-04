# CROC-038 — Regulars: a per-user set of catalog items, restocked onto the shopping list

A user keeps a list of **regulars**: catalog items they buy regularly (toilet
paper, milk, bin bags), each with a quantity and optional unit. A restock
endpoint puts selected regulars on the shopping list, safely repeatable.
Grilled 2026-10-03 together with `crockpot-react` `CFE-015`. "Default items"
in the original ticket is renamed **regulars**.

**Implementation mode**: AI-driven. One piece at a time: failing test, a stub
that fails for the right reason, confirm red, stop for review, implement on
go-ahead, confirm green, stop.

## Facts this rests on (checked 2026-10-03)

- `items` is admin-curated with a unique name (`db/migrations/000001_init.up.sql:82-89`);
  no per-user copies of reference data (`CLAUDE.md` Ownership model).
- `shopping_list_items.item_id` is `NOT NULL REFERENCES items ON DELETE RESTRICT`
  (`000001_init.up.sql:193`), so anything restocked must be a catalog item.
- The catalog already covers household goods: the source data has 387 items,
  29 of them in `House` (Toilet Paper, Kitchen Roll, Bin Bags, Bleach, Shampoo…),
  which is flagged `is_ingredient = false` (`000013`).
- Units include `pack`, `rolls`, `bottle`, `box`, `jars` (`000004_seed_units.up.sql`).
- Manual-add validation: `parseAddManualShoppingListItemRequest` +
  `validateQuantity` (`internal/handler/shopping_list_requests.go:17-42`);
  `unit_not_allowed_for_item` (`shopping_list_handler.go:67`).
- Manual add merges into any row with the same item + unit, recipe-driven
  included, adding quantities and unticking (`FindShoppingListItemForMerge`,
  `IncrementShoppingListItemQuantity` in `internal/sqlc/queries/shopping_list.sql`; `CROC-053`).
- Menu sync touches only non-manual rows: `SyncShoppingListItemQuantities`
  overwrites their quantity; `DeleteObsoleteShoppingListItems` deletes those
  no longer in the menu aggregate (`shopping_list.sql`).
- Regenerate and Clear list remove every row, manual included (`CROC-052`, `CROC-022`).
- Shopping-list writes return `{"message": …}` and the client refetches
  (`shopping_list_handler.go:61,93,117,135,156`).
- Recipe cap: count-then-insert, 409 `recipe_limit_reached`
  (`recipe_handler.go:358-373`). It's a soft cap under concurrency.
- Admin item delete maps an FK violation to 409 `item_in_use` (`item_handler.go:134-141`).
- `TestSchemaFKColumnsAreIndexed` (`internal/repository/schema_test.go:17`)
  checks a hand-maintained list of FK columns lead an index; new FK columns
  must be added to that list. `TestMigrateTruncateCoversEveryReferencingTable`
  (`:208`) requires every table with an FK into a truncated table (here `items`)
  to be in `MigrateTruncate` (`internal/sqlc/queries/migrate.sql`).
- Routes: `main.go:220` (`/shopping-list` group).
- `CROC-022`'s handoff assumed this ticket would own a non-catalog "add anything"
  case. Superseded: see decision 1.

## Decisions

1. **Regulars are catalog items only.** Each is an `item_id` + optional
   `unit_id` + `quantity`. Why: restock must produce shopping-list rows, which
   require a catalog item. Free text would mean letting the list itself hold
   free text (hydrate query, grouping, merging, dismissals, frontend row type).
   Rejected: free-text per-user entries. Cost: a non-catalog item can't be a
   regular, the same limit as "Add something extra" today. Revisit if users
   regularly hit missing items; that's an item-suggestion feature, not this.
2. **Same shape and validation as manual add**: quantity required, unit
   optional and checked against the item's allowed units. Why: restock merges
   on item + unit, so the unit has to be stored; asking for quantities at
   restock time would defeat a one-tap restock.
3. **One regular per item**: `UNIQUE (user_id, item_id)`. Creating one that
   already exists returns 409 `regular_exists`, never an upsert; change it
   with `PATCH`.
4. **Every tier, capped at 50 per user.** 409 `regulars_limit_reached`. A
   count-then-insert soft cap like the recipe cap: concurrent creates can
   overshoot by a handful, which is harmless for a safety cap. Why a cap: it
   bounds a user's rows and every restock transaction.
5. **The restock rule.** For each selected regular, matching list rows on
   item + unit (`IS NOT DISTINCT FROM` for a null unit):

   | List state | Restock does |
   |---|---|
   | No row | Insert a manual row (`is_manual = true`, unticked) with the regular's quantity and unit |
   | Any **unbought** row | **Skip.** Repeat restocks are safe; never adds quantities together |
   | Only **bought** row(s) | Untick the row and set its quantity to the regular's quantity. `is_manual` unchanged |

   Unlike manual add, restock never increments. To get more of something
   already on the list, edit the row's quantity.
   **Accepted tradeoff (founder, 2026-10-03):** resetting a bought
   *recipe-driven* row leaves it recipe-driven, so a later menu change can
   re-sync its quantity or, if the recipe leaves the menu, delete it. The
   restocked item then silently drops off the list. Rejected at the grill: leaving the recipe
   row ticked and inserting a separate manual row (two rows for one item).
   Revisit if the silent removal bites in real use.
6. **Restock silently skips IDs that aren't the caller's regulars.** The query
   is scoped by `user_id`, so foreign IDs are inert. A stale "Add all" from a
   device where a regular was just deleted still restocks everything else.
7. **FKs**: `item_id` and `unit_id` → `ON DELETE RESTRICT` (an item someone
   has as a regular is "in use"; admin delete gets `item_in_use`, same as list
   rows). `user_id` → `ON DELETE CASCADE` (account deletion, `CROC-030`, gets
   it for free).
8. **The "on your list" state is not computed server-side.** `GET /regulars`
   is a plain list. The client derives it from the shopping list it already
   has; the server applies decision 5 regardless.

## API contract

| Endpoint | Body | Success | Errors |
|---|---|---|---|
| `GET /regulars` | — | 200 `[{id, itemId, itemName, categoryId, categoryName, unitId, unitAbbreviation, quantity}]`, ordered by category name then item name | — |
| `POST /regulars` | `{itemId, unitId?, quantity}` | 201 + the regular | 400 `invalid_item_id` / `invalid_unit_id` / `unit_not_allowed_for_item` / existing quantity errors; 409 `regular_exists`; 409 `regulars_limit_reached` |
| `PATCH /regulars/:id` | `{unitId: string \| null, quantity}`, a full replacement: `quantity` required, absent or null `unitId` clears the unit | 200 + the regular | 400 as above; 404 `regular_not_found` |
| `DELETE /regulars/:id` | — | 204 | 404 `regular_not_found` |
| `POST /shopping-list/restock` | `{regularIds: [...]}` | 200 `{message}` | 400 for an empty array or more than 50 IDs, or a malformed ID |

All routes are behind auth middleware; every query is scoped to the caller's
`user_id`, and another user's regular is a 404, not a 403. Restock runs in one
`WithinTx` and gets-or-creates the shopping list.

## Schema

Migration `000014_regular_items`:

- `regular_items (id UUID PK, user_id UUID NOT NULL → users ON DELETE CASCADE,
  item_id UUID NOT NULL → items ON DELETE RESTRICT, unit_id UUID NULL → units
  ON DELETE RESTRICT, quantity NUMERIC(10,2) NOT NULL, created_at, updated_at,
  UNIQUE (user_id, item_id))`.
- Indexes on `item_id` and `unit_id` (`user_id` is covered by the unique index's
  leading column).
- Add `regular_items` to `MigrateTruncate`.

## Acceptance criteria

- [ ] Migration up/down clean; `TestSchemaFKColumnsAreIndexed` and
      `TestMigrateTruncateCoversEveryReferencingTable` green with the new table.
- [ ] CRUD per the contract, including 409 `regular_exists`, 409 at the 51st
      create, 404 for another user's regular on `PATCH`/`DELETE`.
- [ ] Restock, one repository test per row of decision 5: absent → inserted
      manual and unticked; unbought (manual and recipe-driven) → untouched;
      bought manual → unticked, quantity = regular's; bought recipe-driven →
      unticked, quantity = regular's, still `is_manual = false`; null unit
      matches null unit only.
- [ ] Restock twice in a row leaves the list identical after the second call.
- [ ] Restock with a mix of own and foreign/unknown IDs applies only the
      caller's and returns 200.
- [ ] Restock is atomic: a failure mid-way leaves the list unchanged.
- [ ] Admin deleting an item that is someone's regular gets 409 `item_in_use`.
- [ ] `requests/regulars.http` (CRUD, 409s, cleanup) and a restock section in
      `requests/shopping-list.http`, run top to bottom against local.

## Non-goals

- Non-catalog or user-created items.
- A "save this row as a regular" action from the shopping list.
- Auto-restock on Regenerate or Clear list (both still wipe restocked rows).
- Reordering regulars, or any grouping beyond category.
- Unit conversion when matching (`CROC-054`'s territory).

## Verification

- **Logic with assertable behaviour**: handler tests with mockery mocks,
  `go test ./internal/handler/...`; repository tests against Neon,
  `./scripts/test-repo.sh -run 'Regular|Restock|Schema|MigrateTruncate'`, then
  the full suite.
- **Service boundary**: `requests/regulars.http` and the restock section of
  `requests/shopping-list.http` against the real local API.
- **Limits**: the 50 cap exercised through the `.http` suite (create until 409),
  not only a unit test.
- Lint: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`.
- Review: `branch-review` against `main`.

## Sequencing

Build before `CFE-015`; it doesn't wait for the designs. Suggested pieces:
migration + schema tests → repository CRUD → restock repository → handlers +
routes → `.http` files.

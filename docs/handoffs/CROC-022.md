# CROC-022 — Manual items, obtain-toggle, quantity edit, clear list

**Implementation mode: AI-driven.** Claude works one piece at a time:
failing test(s) first, a stub that fails for the right reason, confirm
red, stop for review, implement on go-ahead, confirm green, stop again
before the next piece.

## Summary

Adds the write endpoints `CROC-021` deliberately left out: manually
adding an item to the shopping list, editing any item's quantity
(manual or recipe-driven), toggling `obtained`, removing a single item,
and clearing the whole list. All four endpoints scope to the
authenticated user's own `shopping_lists` row; nothing here changes
`Regenerate`'s existing behaviour — this ticket writes to the same
tables it already reads/writes, through the schema and dismissal
machinery `CROC-021` already built.

## Decisions from the interview

### 1. Manual add is catalog-scoped: `itemId` + `quantity` + optional `unitId`

`shopping_list_items.item_id` is a `NOT NULL` FK to `items`
(`db/migrations/000001_init.up.sql:192`), and `GET /shopping-list`'s
categorised grouping depends on every row resolving to a real item (for
`itemCategoryId`/`itemCategoryName`). A freeform, non-catalog "add
anything" box would need a schema change and would have no category to
display under — out of scope here, and already the express reason
`CROC-038` ("default items" — toilet paper, bin bags, anything not tied
to a recipe) exists as its own separate, not-yet-built ticket. This
ticket's manual add stays within the existing items catalog; `CROC-038`
owns the non-catalog case.

**Correction from an earlier pass of this doc**: initially said no
application-level unit validation exists anywhere, based only on
`recipe_requests.go`'s handler-layer parsing (UUID-shape + `quantity >
0`, nothing more). That's the wrong layer to check — `recipe.go:492-525`
has `checkAllowedUnits`, called from both `Create` and `Update`
(`recipe.go:315,371`), which does reject a unit absent from its item's
`allowedUnitIds` (empty set = unconstrained, nil unit always passes).
Manual add reuses this exact helper for the same check, for the same
reason recipe ingredients get it: an item's allowed-units set is the
existing mechanism for "this quantity/unit combination makes sense for
this item", already enforced everywhere else an item+unit pair is
written.

### 2. Adding a duplicate item merges only across manual-to-manual; never manual-to-generated

Adding "onions" via manual add when a **manual** "onions" row already
exists (same `itemId/unitId`) increments that row's quantity — safe,
since manual rows are never touched by regeneration.

Adding "onions" when only a **generated** (recipe-driven) "onions" row
exists always inserts a *new*, separate manual row — never merges into
the generated row. Regeneration's `SyncShoppingListItemQuantities`
(`shopping_list.sql:60-71`) unconditionally overwrites every non-manual
row's `quantity` with the freshly recalculated recipe total on every
menu change; folding a manual addition into that same row would mean it
gets silently overwritten the next time an unrelated recipe is added or
removed — no warning, no trace. Keeping the two rows separate protects
the manual addition the same way `CROC-021` already protects `obtained`
and dismissals.

**The founder wants these to visually read as one combined line** (e.g.
one "onions: 4" instead of two rows). That's achievable with **no
schema change**: `GET /shopping-list` keeps returning both rows exactly
as today (each with its own `id`, independently obtainable/removable);
summing rows that share an `itemId`/`unitId` into one displayed line is
a rendering choice for whichever ticket builds the full shopping-list
screen (`CFE-009`) — flagged there as a forward-pointer, not solved here.
Same null-safe matching as `CROC-021` (`unit_id IS NOT DISTINCT FROM`,
`db/migrations/000011...`) — can't use `ON CONFLICT` for the same reason
decision 3 of that handoff gives (Postgres treats two `NULL`s as
not-equal for conflict matching), so the merge-into-existing-manual-row
check is an application-level select-then-update/insert, same shape as
`Regenerate`'s own multi-statement approach.

### 3. Quantity is editable on any row — manual or recipe-driven — and is **not** protected against regeneration

Checked the actual old-app screen (`crockpot/src/app/your-crockpot/
components/Menu/ShoppingList.tsx`), not just its data layer: every row,
manual or generated, has the same editable-quantity control
(`ShoppingListRowEditor`) — no `isManual` gating anywhere in the UI.
This is a real, used feature ("recipe says 2 onions, I know I'll want
6"), not a manual-items-only convenience.

Traced what actually happens to a hand-edited *generated* row's quantity
across a regenerate: the old app's `rebuildShoppingListForUser` only
preserves `isManual` items wholesale — every generated row is recomputed
from scratch on every menu write, silently overwriting any hand-edit
back to the recipe-calculated amount. Confirmed this codebase's
equivalent (`SyncShoppingListItemQuantities`) does the same. Explicitly
discussed making this sticky (extending the regen SQL to preserve a
hand-edited generated quantity, the same way `obtained` was deliberately
made sticky at `CROC-021`) — **decided against**: accept the same
behaviour the old app already has. A hand-edited generated-row quantity
resets to the recipe's calculated amount on the next regenerate
triggered by *any* menu change, including an unrelated one. Documented
here as an accepted, known tradeoff, not a bug to fix later.

### 4. One remove endpoint, not two — behaviour branches internally on `is_manual`

`DELETE /shopping-list/items/:id`: looks up the row first. If
`is_manual`, plain delete. If not, delete **and** insert a
`shopping_list_dismissed_items` row (`item_id`, `unit_id`,
`quantity_at_dismissal` = the row's quantity at the moment of deletion),
both in one transaction — reusing `CROC-021`'s existing schema and its
already-fully-wired skip logic in `InsertNewShoppingListItems`
(`shopping_list.sql:76-92`, the `NOT EXISTS ... shopping_list_dismissed_items`
clause) unchanged. `CROC-021`'s own handoff (decision 8) already named
this "CROC-022's delete-a-generated-item endpoint" in the singular — one
button on the screen, the manual/generated distinction is invisible to
the user and handled entirely server-side.

### 5. `DELETE /shopping-list` ("clear list") is in scope, and does **not** write dismissals

Wasn't named in any existing ticket line, but is a real, prominent
control in the design (`yp1.png`) and existed in the old app
(`clearAllItemsFromShoppingList`) — a genuine gap, folded into this
ticket rather than split out, since it reuses the same delete
machinery being built here anyway.

Deletes every row (manual and generated) unconditionally, writes **no**
`shopping_list_dismissed_items` rows. Distinct on purpose from the
single-item remove's sticky behaviour: "clear list" reads as a
fresh-start gesture, not "permanently suppress everything I had", and
matches the old app's actual (simpler, non-sticky) behaviour. A
recipe still on the menu after a clear will reappear on the very next
regenerate, same as before this ticket.

### 6. `PATCH /shopping-list/items/:id` accepts `obtained` and/or `quantity`, at least one required

One endpoint, partial-update body — `{"obtained"?: bool, "quantity"?:
number}`, at least one field present or `400`. Matches this ticket's own
original line ("obtained toggle (`PATCH /shopping-list/items/:id`)")
extended to also carry the now-confirmed quantity-edit capability,
rather than a second endpoint for the same row.

### 7. Bulk mark-obtained — dropped

No design screen or old-app precedent shows anything like a bulk-obtain
action (checked both explicitly). Confirmed with the founder this was
likely loose wording in the original ticket line, not an intended
feature — dropped from scope. The one-row-at-a-time `PATCH` covers the
real need.

### 8. Response shape and status codes

All four new endpoints return `200 {"message": "..."}`, matching
`MenuHandler`'s existing convention for this same resource family
(`menu_handler.go`) rather than `POST /items`/`POST /recipes`'s
`201 + resource body` pattern — `crockpot-react`'s mutation hooks
(`useAddToMenu.ts` etc.) already manage their own optimistic cache
updates and don't depend on the response body's shape, same as every
existing menu-write hook.

### 9. Ownership scoping

Every endpoint scopes through `shopping_list_items.shopping_list_id ->
shopping_lists.user_id = <authenticated user>`. An id that exists but
belongs to another user's list returns `404`, not `403` — consistent
with not confirming another user's row exists at all (same posture as
`CROC-019`/`CROC-021`'s own not-found handling).

## Non-goals

- Freeform/non-catalog manual items — `CROC-038`.
- A "default items" saved/reusable set — `CROC-038`.
- Making a hand-edited generated-row quantity survive regeneration —
  explicitly decided against (decision 3).
- Bulk mark-obtained — dropped (decision 7).
- Visually merging a manual + generated row sharing the same item into
  one displayed line — `CFE-009`'s frontend concern (decision 2);
  backend keeps them as separate rows/ids.

## Acceptance criteria

- [x] `POST /shopping-list/items` with a valid `itemId`/`quantity` (no
      unit) creates a new manual row; `GET /shopping-list` reflects it,
      correctly categorised.
- [x] `POST /shopping-list/items` for an `itemId`/`unitId` that already
      has an existing **manual** row increments that row's quantity
      instead of creating a second row.
- [x] `POST /shopping-list/items` for an `itemId`/`unitId` that already
      has a **generated** (non-manual) row still creates a new, separate
      manual row — no merge across the manual/generated boundary.
- [x] `POST /shopping-list/items` with an unknown `itemId` or `unitId`
      returns a `4xx`, never a `500`.
- [x] `POST /shopping-list/items` with a `unitId` not in the item's
      `allowedUnitIds` returns a `4xx` (reusing `checkAllowedUnits`).
- [x] `POST /shopping-list/items` with `quantity <= 0` returns `400`.
- [x] `PATCH /shopping-list/items/:id` with `{"obtained": true}` flips
      the row's `obtained`; a subsequent `GET` reflects it.
- [x] `PATCH /shopping-list/items/:id` with `{"quantity": N}` updates
      quantity on both a manual row and a generated row.
- [x] `PATCH /shopping-list/items/:id` with neither field present
      returns `400`.
- [x] `PATCH`/`DELETE` on an id belonging to another user's list returns
      `404`.
- [x] `DELETE /shopping-list/items/:id` on a manual row deletes it
      outright; no dismissal row is written.
- [x] `DELETE /shopping-list/items/:id` on a generated row deletes it
      and writes a `shopping_list_dismissed_items` row with the correct
      `quantity_at_dismissal`; a subsequent regenerate from an unrelated
      menu change does not bring it back while the required quantity is
      unchanged.
- [ ] `DELETE /shopping-list` removes every item (manual and generated)
      in one call and writes no dismissal rows; a subsequent regenerate
      freely reintroduces any still-needed recipe-driven item.
- [ ] A hand-edited quantity on a generated row is overwritten back to
      the recipe-calculated amount by the next regenerate triggered by
      an unrelated menu change (the accepted decision-3 tradeoff,
      exercised as a real regression check, not just documented).

## Verification modes

- **Service/API boundary** (`~/.claude/CLAUDE.md`): real requests
  against the real Neon dev DB, per piece, not batched at the end —
  same mode `CROC-021` used. `./scripts/test-repo.sh -run <TestName>`
  for the repository-layer manual-add merge logic, the delete/dismissal
  write, and the clear-list wipe. `go test ./internal/handler/...` for
  handler-layer request validation, ownership-scoping, and wiring.
- **Manual regression**: extend `requests/shopping-list.http` —
  replace the existing placeholder ("tick obtained via direct SQL,
  swap for the real endpoint once CROC-022 ships") with the real
  `PATCH` call, and add sections for manual add (new row + merge into
  an existing manual row), quantity edit (both a manual and a generated
  row, including confirming the generated-row edit really does reset on
  the next regenerate), remove (manual vs. generated, confirming the
  dismissal survives an unrelated regen), and clear list. Run
  end-to-end against a running server.

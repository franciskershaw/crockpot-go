# CROC-021 — Generate/regenerate shopping list from current menu

**Implementation mode: AI-driven.** Claude works one piece at a time:
failing test(s) first, a stub that fails for the right reason, confirm
red, stop for review, implement on go-ahead, confirm green, stop again
before the next piece.

## Summary

Builds `GET /shopping-list` and wires automatic, transactional
shopping-list regeneration into `CROC-019`'s three existing menu-entry
write handlers (`UpsertEntry`/`UpdateEntryServes`/`RemoveEntry`).
Aggregates `recipe_ingredients` across every recipe on the menu, scaled
by each entry's `serves` ratio, merging same-item ingredients that use
different-but-compatible metric units (grams+kilograms; millilitres,
litres, tablespoons, teaspoons, cups, pints) into one combined line.
Preserves `obtained` state across regenerations, and supports a user
permanently dismissing a generated item from the list — "sticky" only
for as long as the required quantity doesn't change. Manual items,
obtain-toggle, and the dismiss endpoint itself are `CROC-022`.

## Decisions from the interview

### 1. Regeneration is automatic and transactional, not a separate endpoint

The old app (`crockpot/src/data/shopping-list/shoppingList.ts`) calls
`rebuildShoppingListForUser` from inside every menu mutation
(`menuMutations.ts:110,150,182`) rather than exposing regeneration as
something the frontend triggers itself. Kept that shape: an explicit
`POST /shopping-list/regenerate` the frontend must remember to call
after every menu write creates a window where the two resources
disagree until the caller remembers the second round-trip, in three
separate places.

Wrapped in the codebase's existing multi-write pattern —
`Transactor.WithinTx` (`auth_handler.go:69-72`), already used for
exactly this shape of problem: `recipe_handler.go:65,94,118` (recipe +
ingredients + categories) and `item_handler.go:69,118` (item + allowed
units). `MenuHandler`'s three write methods each wrap their existing
menu write and the shopping-list regen in one `WithinTx` call — same
tool, new pair of repositories, no change to either handler's
request/response shape.

### 2. Aggregation runs in SQL, not fetched into Go and summed

Matches this codebase's own precedent for moving off the old app's
Mongo-embedded-array-workaround shape: `CROC-042` moved relevance
scoring into a single SQL query specifically because a real relational
schema makes server-side aggregation possible where Mongo's embedded
documents didn't allow it (`master-spec.md:516`). The old app's
`Map`-based loop in `shoppingList.ts:56-80` was a workaround for that
constraint, not a pattern to carry forward now that ingredients live in
`recipe_ingredients`. One query over
`recipe_menu_entries JOIN recipes JOIN recipe_ingredients`, computed and
grouped in SQL, inside the same transaction as the menu write.

### 3. Regeneration preserves `obtained`; three-statement NULL-safe sync instead of `ON CONFLICT`

The old app resets every generated item's `obtained` to `false` on
every regenerate (`shoppingList.ts:82-88`) — a real, easily-hit bug:
change one recipe's serving size and the *entire* list, including
unrelated items you'd already ticked off, resets. Diverging from that
on purpose: an item that's still needed after a regenerate keeps its
`obtained` value; only items whose quantity actually changed, or that
dropped off the menu entirely, are touched.

Can't implement this as a single `INSERT ... ON CONFLICT DO UPDATE`,
because `recipe_ingredients.unit_id` is nullable (an ingredient like "2
eggs" has no unit — `db/migrations/000001_init.up.sql:129` has no `NOT
NULL`), and Postgres treats two `NULL`s as not-equal for
conflict-matching, so a unique index on `(shopping_list_id, item_id,
unit_id)` would never fire a conflict for any unit-less ingredient — the
item would just accumulate duplicate rows on every regen instead of
preserving state. Verified this is a real, common case against the live
Neon data, not a hypothetical: several ingredients (`Honey`,
`Coriander (Fresh)`, `Mustard (Dijon)`, `Sesame Seeds`, `Olive Oil`,
`Parsley`, `Ginger`, `Hot Sauce`) appear with a `NULL` unit alongside
measured units across different recipes.

Instead, three statements per regen, matched with `IS NOT DISTINCT FROM`
(Postgres's null-safe equality) on `unit_id`:
1. `UPDATE` rows whose `(item_id, unit_id)` still appears in the fresh
   aggregate — sets `quantity` only, leaves `obtained` untouched.
2. `INSERT` rows from the aggregate that don't yet exist as active
   items (see decision 5 for how "active" accounts for dismissals).
3. `DELETE` existing non-manual rows whose `(item_id, unit_id)` no
   longer appears in the aggregate at all (recipe removed from menu, or
   its need for that ingredient zeroed out).

All three scoped to `NOT is_manual` — manual items (`CROC-022`) are
untouched by regeneration.

### 4. `CROC-021` owns `GET /shopping-list`

Neither this ticket's nor `CROC-022`'s line names a read endpoint —
`CROC-022`'s only names `PATCH /shopping-list/items/:id`. Following
`CROC-019`'s precedent of bundling a resource's write path with its read
path in the same ticket rather than splitting them (`CROC-019.md`
scopes `GET /menu` in, deliberately scopes `menu_history_entries` writes
and this ticket's regen out): the ticket that makes `shopping_list_items`
rows exist and stay correct is the one that has to expose reading them
back, both because `CFE-006`/`CFE-009` need it and because there's no
way to verify this ticket end-to-end otherwise. Never writes; lazily
creates the `shopping_lists` row on first regen via the same
`GetOrCreateMenu`-shaped `ON CONFLICT DO UPDATE ... RETURNING id` trick,
not on `GET`.

### 5. Response is hydrated, reusing `HydratedIngredient`'s shape

`RecipeDetail`'s `HydratedIngredient` (`internal/models/recipe.go:57-65`)
already solved this exact problem for recipe ingredients: item name,
item category id+name (for grouping), unit abbreviation, not bare IDs.
`CFE-009` explicitly needs "categorised" display
(`crockpot-react/docs/specs/master-spec.md:252`), which needs
`itemCategoryId`/`itemCategoryName` per row — grouping itself is
`CFE-009`'s frontend concern, this ticket only supplies the field.
Shopping-list item DTO = same shape plus `id` (the row, for `CROC-022`'s
`PATCH`), `obtained`, `isManual`.

### 6. No `recipe.serves` zero-guard

The old app guards `baseServes = recipe.serves || 1` before dividing
(`shoppingList.ts:66`) because Mongo/Prisma had no server-side
validation forcing `serves ≥ 1`. This codebase already does:
`parseCreateRecipeInput` rejects `Serves < 1` at creation
(`recipe_requests.go:50-53`), and `CROC-019` confirmed a menu entry's
`serves` is validated against the identical 1-50 bound. Every
`recipes.serves` value is guaranteed ≥1 at write time — no guard for a
state the schema can't reach.

### 7. Cross-unit merging for compatible metric units (not a straight port of the old app's per-unit split)

Verified against live data that this matters, not a hypothetical:
querying the real Neon dev DB, `Honey` appears across 4 different units
(none, grams, ml, tablespoons) and a dozen+ other common ingredients
(`Sugar (Brown)`, `Flour (White)`, `Soy Sauce`, `Butter`, `Mayonnaise`,
...) appear across 3. Grouping strictly by `(item_id, unit_id)` — the old
app's exact key, `shoppingList.ts:70` — would put each of those on the
list as multiple disconnected rows for what's really one ingredient.

`units` (`db/migrations/000001_init.up.sql:74-80`) currently has no
conversion metadata — just `name`/`abbreviation`. Adding:
- `dimension` (`mass` | `volume` | `count`) per unit. Mass: `grams`,
  `kilogram`. Volume: `milliliters`, `litres`, `cup`, `tablespoons`,
  `teaspoons`, `pint`. Count (never merges across different `unit_id`s,
  same as a `NULL` unit): `bottle`, `box`, `cans`, `cloves`, `cobs`,
  `fillets`, `jars`, `loaves`, `pack`, `rolls`, `sachet`, `slices`.
- `base_factor NUMERIC`: factor to convert one of this unit into the
  dimension's base unit (grams for mass, millilitres for volume; `1` and
  irrelevant for `count`). Metric only, UK conventions (the recipe
  corpus reads UK — `Coriander (Fresh)`, `Mustard (Dijon)`, `Greek
  Yoghurt`, `Creme Fraiche`): `kilogram → 1000`, `litres → 1000`,
  `tablespoons → 15`, `teaspoons → 5`, `cup → 250`, `pint → 568`,
  `milliliters → 1` (own base), `grams → 1` (own base).

Aggregation groups mass/volume-dimension ingredients by `(item_id,
dimension)`, summing `quantity * base_factor * serves_scale`, and
outputs the row under that dimension's base unit (`grams`'s or
`milliliters`'s own `unit_id`) — not whichever unit a given recipe
happened to use. Count-dimension and `NULL`-unit ingredients keep
grouping by literal `(item_id, unit_id)`, unconverted, exactly as
decision 3 already establishes.

Imperial units, and any per-user/per-recipe metric-vs-imperial
preference, are explicitly out of scope (see Non-goals) — flagged as a
plausible future ticket once this ships, not folded in here.

### 8. Removing a generated item is sticky — but scoped to the quantity that was dismissed

Surfaced as a real gap, not an old-app port: the old app *can* delete a
generated row (`removeItemFromShoppingList` works on manual or generated
items via an `isManual` filter, `shoppingList.ts:174-203`), but nothing
records that the deletion was deliberate, so the very next regenerate —
triggered by any menu change, even an unrelated one — just re-aggregates
and silently re-inserts it. Removing noise (oil, salt, butter, spices
already owned) before a shop is core to the feature's value, so this
needs to actually stick, but only as long as the underlying need hasn't
changed: if a newly-added recipe needs more flour than was already
covered, that's meant to surface again as a fresh decision, not stay
silently suppressed — regeneration reintroducing a dismissed item at a
new quantity **is** the intended "undo" for a stale dismissal, not a bug
to prevent.

New table `shopping_list_dismissed_items (shopping_list_id, item_id,
unit_id, quantity_at_dismissal)` — kept separate from
`shopping_list_items` rather than a flag on it, so the visible list
table only ever holds currently-active rows and `GET /shopping-list`
stays a plain, unfiltered select. `CROC-022`'s delete-a-generated-item
endpoint deletes the visible row and writes a dismissal row capturing
the quantity at that moment, in one transaction (its write, this
ticket's table). This ticket's ordering step (decision 3's INSERT) skips
an aggregate row only when a dismissal exists for that
`(item_id, unit_id)` **and** its `quantity_at_dismissal` still exactly
matches the freshly-aggregated quantity; on a mismatch the item
reappears as a normal new row (unobtained — it's a new requirement) and
the now-stale dismissal row is deleted in the same step. An
never-invalidated dismissal (the need for that item never changes)
holds indefinitely, including across the item briefly dropping off the
menu and reappearing at the same quantity.

## Non-goals

- Manual item add/remove, obtain-toggle, bulk-mark-obtained, and the
  delete-a-generated-item endpoint itself (writes the dismissal row per
  decision 8) — all `CROC-022`.
- `menu_history_entries` writes — `CROC-020`.
- Imperial units, or any per-user/per-recipe metric-vs-imperial
  preference. Real future scope, not this ticket: would need new
  imperial unit rows (none exist today — no `oz`, `lb`, `fl oz`), a
  system tag per unit (reopening the same UK/US ambiguity resolved here
  for `cup`/`pint`, but as a systemic axis), a preference field, and a
  migration to backfill the 387 existing recipes with no reliable signal
  to infer their authored system from (default-all-one-way, let the
  founder correct outliers).
- Categorised grouping/display on the frontend — `CFE-009`'s job; this
  ticket supplies `itemCategoryId`/`itemCategoryName` per row so the
  frontend can group, doesn't group itself.

## Acceptance criteria

- [ ] `POST /menu/entries`, `PATCH /menu/entries/:recipeId`,
      `DELETE /menu/entries/:recipeId` each regenerate the shopping list
      in the same transaction as the menu write; a regen failure rolls
      back the menu write too.
- [ ] Two recipes both needing grams of the same item combine into one
      row with the summed quantity.
- [ ] Two recipes needing the same item in different but compatible
      metric units (e.g. tablespoons + millilitres) combine into one row
      in the dimension's base unit, with a correctly-converted summed
      quantity.
- [ ] Two recipes needing the same item where one specifies no unit and
      the other does remain two separate rows (no merge across the
      `count`/`NULL` boundary).
- [ ] An item already on the list ticked `obtained = true` stays
      `obtained = true` after an unrelated menu change regenerates the
      list.
- [ ] An item whose required quantity changes on regen keeps its row
      (not deleted-and-reinserted) and gets the new quantity.
- [ ] An item no longer needed by any menu recipe is removed from the
      list on the next regen; manual items are never touched by regen.
- [ ] A dismissed item does not reappear on a regen that leaves its
      required quantity unchanged.
- [ ] A dismissed item reappears, `obtained = false`, at the new
      quantity, on a regen where its required quantity changes — and the
      stale dismissal record is cleaned up.
- [ ] `GET /shopping-list` for a user with no shopping list row yet
      returns `200` with empty items, without creating a row.
- [ ] `GET /shopping-list` response includes `itemCategoryId`/
      `itemCategoryName`/`unitAbbreviation` per item (hydrated, not bare
      IDs).
- [ ] `recipes.serves` divide-by-zero is not guarded against in the
      aggregation query.

## Verification modes

- **Service/API boundary** (`~/.claude/CLAUDE.md`): real requests
  against the real Neon dev DB, per piece, not batched at the end.
  `./scripts/test-repo.sh -run <TestName>` for the repository-layer
  regeneration query — fixture recipes/menu-entries with known
  ingredient quantities, deliberately including a unit-less ingredient
  (exercises the `IS NOT DISTINCT FROM` path) and a cross-unit pair
  within one dimension (exercises the conversion path). `go test
  ./internal/handler/...` for handler-layer wiring — the three menu-write
  handlers actually calling regen inside `WithinTx`, and
  `GET /shopping-list` response parsing.
- **Limits/thresholds**: the dismissal quantity-match boundary (exact
  match holds, any difference invalidates) exercised through the real
  repository query with fixtures at, and just off, the dismissed
  quantity — not asserted synthetically in isolation from the SQL that
  computes it.
- **Manual regression**: `requests/menu.http` (existing) alongside a new
  `requests/shopping-list.http` — add a recipe to the menu, confirm
  `GET /shopping-list` reflects it; tick an item obtained; bump that
  recipe's serves; confirm the ticked item is still obtained and its
  quantity updated; remove the recipe from the menu; confirm the item is
  gone. Run end-to-end against a running server.

Completed 2026-09-18.

# CROC-024 — source data review

Generated from the MongoDB Compass export (`crockpotV3.*`, 2026-08-31).
Companion to `CROC-024.md` — the concrete profile of the source data,
the known quirks, and how the tool handles each. No manual data edits
required; the founder only needs to export the `Account` collection.

## Export profile

| Collection | Docs | Notes |
| --- | --- | --- |
| `ItemCategory` | 13 | names match the seed exactly |
| `Unit` | 21 | 20 match the seed; 1 junk `{name:"", abbreviation:""}` row (skipped, not aborted on — CROC-011 lesson) |
| `RecipeCategory` | 28 | names match the seed exactly |
| `User` | 42 | 2 real (Francis, Zoe — both ADMIN), 40 scraper/spam signups |
| `Item` | 387 | 366 have non-empty `allowedUnitIds`; 0 entries reference a dead unit |
| `Recipe` | 213 | all `approved: true`; all images `res.cloudinary.com`; all 1–3 categories; all ≥1 ingredient; 0 unresolved item/unit refs |

Extended JSON form: `{"$oid": "..."}`, `{"$date": "ISO-8601 string"}`.

## Recipe creators

| `createdById` | recipes | dates |
| --- | --- | --- |
| `68740e93cd88665fea847576` — **not in `User`** | 189 | all 2025-07-13 (bulk seed import) |
| Zoe Thexton (`68eb87d8929571824b1516bd`) | 22 | 2025-12 → 2026-07 |
| Francis Kershaw (`68e93f253533a3d30146ba07`) | 2 | 2026-07 / 08 |

The ghost id is a bulk seed-import account (its own ObjectId timestamp is
2025-07-13 19:52, same evening all 189 recipes landed). Migration maps it
to a synthetic **`Crockpot`** ADMIN user — see `CROC-024.md` decision 4.

Consequence for `created_at`: 189 of 213 recipes share one date, so
within that block `GET /recipes` order falls to the `id` tiebreak
(UUIDv5 — stable pseudo-random). Worth knowing for CROC-042 (the old app
faked exactly this with `hashString(recipe.id + seed)`).

## Item `allowedUnitIds` gaps — applied by the tool

23 recipe ingredients across 22 recipes use a unit not in that item's
`allowedUnitIds`. Founder's call (2026-08-31): all are reasonable —
**widen the item's allowed units**. The tool does this via a hard-coded
`allowedUnitAdditions` table in `cmd/migrate-data/fixups.go` (CROC-024.md
decision 7) — no manual Mongo edits. The 11 entries, one unit each:

| Item | Add unit | Recipes affected |
| --- | --- | --- |
| Mayonnaise | `milliliters` | Air Fryer Chicken Burger, BBQ Chicken And Spicy Sweetcorn Fritters, Buffalo Fried Chicken Burgers with Pickles, Crispy Chicken Wraps With Chilli Mayo, Fish Finger Sarnie With Dill Mayo, Pulled Chicken Burgers, Sticky Honey Mustard Posh Dogs |
| Tomatoes (Passata) | `cans` | Bacon Mushroom and Lentil Pie, Beef Rogan Josh Style Curry, Mexican Bean Stew, Roasted Sweet Potato and Kidney Bean Chilli |
| Tomatoes | `cans` | Firecracker Beans, Seafood Paella |
| Tomatoes (Cherry) | `cans` | Vegetable pasta bake |
| Stock Cube (Chicken) | `milliliters` | Crispy Chicken & Black Bean Sauce, Lemon Chicken, Sweet Potato Katsu Curry — quantity = the volume of stock the cube makes up |
| Mustard (Yellow) | `milliliters` | Firecracker Beans |
| Almonds | `tablespoons` | Beef Apricot Koftas |
| Lemon | `tablespoons` | Beetroot hummus |
| Lime | `tablespoons` | Ma Hor |
| Thyme (Fresh) | `teaspoons` | Pork Souvlaki Burgers with Feta Drizzle |
| Hoisin Sauce | `grams` | Shredded Hoisin Chicken Wraps |

With this table applied, the stray-unit count is 0 and no recipe is
skipped on unit grounds. A stray unit *not* in the table would still
trip decision 6's skip-recipe + loud report.

## Duplicate ingredients (2 recipes)

The new schema's `UNIQUE (recipe_id, item_id)` forbids what Mongo's
embedded array allowed. Migration keeps the **first** entry and drops the
rest, reporting each.

| Recipe | Item | Entry A | Entry B | Outcome |
| --- | --- | --- | --- | --- |
| BBQ Pulled Pork | Burger Buns | `6` | `6` | keep `6` — looks like a double-entry bug ✓ |
| Sweet & Sour Chicken | Cornflour | `3 tbsp` | `1.5 tbsp` | keep `3 tbsp` (tool default); tell Claude to special-case it to `4.5 tbsp` if both uses are real |

## Users not migrated (40)

All role `FREE`, no `name`, no favourites, corporate/scraper email
domains (safeguardglobal.com, korper.nl, naturalretreats.com, …). Left
behind entirely this pass.

# CROC-042 — Recipe relevance ranking

Grilled 2026-09-06, AI-driven, not yet built.

## Summary

Replaces the current always-`created_at DESC` ordering on `GET /recipes`
with three modes selected by which filters are active: scored ordering
(coverage-based, when ingredients and/or categories are selected),
seeded-random ordering (when nothing is selected), and the existing
plain default (when only `q` and/or time range are set, since those
stay hard filters with no scoring signal). Adds match-explanation fields
to `RecipeCard` for `CFE-021` to consume. Explicitly does not port the
old app's flat absolute-count algorithm (`crockpot/src/data/recipes/relevance-cache.ts`)
or add any dietary/allergen distinction — see Non-goals.

## Decisions from the interview

1. **No `is_dietary`/category-kind schema change.** Raised early
   (why does selecting "Veggie" surface meat recipes?) but resolved as
   out of scope: that's a hard-exclude concern (allergen/dietary), a
   different filter semantic from every other category, and probably a
   different data shape (`recipe` columns or a `recipe_dietary_flags`
   table, not another `recipe_categories` row). Categories are scored
   uniformly, no special-casing. Documented as a parked future idea in
   `docs/specs/master-spec.md` (next to `CROC-038`), not built here.

2. **Ingredient scoring is coverage of the recipe's own ingredient
   list**, not the old app's flat absolute count:
   `ingredientCoverage = matchedIngredientCount / totalIngredientCount`
   (recipe's total, from `recipe_ingredients`). Fixes the old
   algorithm's real defect — a recipe you can almost entirely make from
   your selection should outrank a big recipe that shares a few
   ingredients incidentally. No missing-ingredient penalty for now
   (recipes with extra ingredients you don't have aren't penalized,
   just not boosted further).

3. **Category scoring is coverage of what the user selected**, not the
   recipe's own tag count: `categoryCoverage = matchedCategoryCount /
   categoriesSelected`. Normalizing against the user's selection (not
   the recipe's total categories) means picking all of your chosen
   categories yields a perfect 1.0, independent of how many other tags
   the recipe happens to carry.

4. **Combined score is a selection-count-weighted average, not a fixed
   constant split.** Rejected a flat 0.7/0.3 ingredient/category weight
   as an arbitrary number with no grounding — it would let a strong
   category match (e.g. 3 of 5 selected) get crushed to near-nothing
   just because the constant said categories count less. Instead, each
   axis's weight is how many items the user selected on it — a bigger
   selection is a more deliberate statement and earns more say in the
   score:
   ```
   I = len(ingredientIds selected), C = len(categoryIds selected)

   I > 0 && C > 0:  score = (ingredientCoverage*I + categoryCoverage*C) / (I + C)
   I > 0 && C == 0: score = ingredientCoverage
   I == 0 && C > 0: score = categoryCoverage
   I == 0 && C == 0: no score — see ordering modes below
   ```
   Known tradeoff, accepted: this treats one selected category and one
   selected ingredient as equally weighty "votes," which isn't strictly
   true (a category is a broader statement than a single pantry item).
   No fabricated exchange rate was preferred over this simplification;
   revisit with real usage data if it feels off in practice.

5. **`q` (name search) and time range stay hard `AND` filters, unchanged
   — confirmed via `recipes.sql:64-66`, not assumed.** They don't feed
   the score at all: every row in a filtered result set already
   satisfies them, so a "bonus" for matching would be non-discriminating
   noise. This resolves the appendix's open "interaction with `q`"
   question with no new behaviour needed.

6. **Scoring runs in SQL, not fetch-and-score in Go.** Verified against
   the real dev DB before deciding: 213 recipes today, max 21
   ingredients on any one recipe — small enough that either approach is
   instant right now, but SQL-side is the one that scales, because it
   preserves real `LIMIT`/`OFFSET` pagination against the scored
   `ORDER BY`. Fetch-and-score would mean pulling every candidate into
   the app before sorting/slicing — the exact pattern the old app's
   Mongo-necessitated `unstable_cache` was working around, not a
   pattern to reintroduce now that Postgres can compute this natively.
   Indexes already support it with no migration needed: `recipe_ingredients`
   has `UNIQUE (recipe_id, item_id)` (`000001_init.up.sql:124-132`) and
   `idx_recipe_ingredients_item_id` (`000008_add_fk_indexes.up.sql:4`);
   `recipe_categories_recipes` has `PRIMARY KEY (recipe_id, category_id)`
   (`000001_init.up.sql:134-139`). Scoring only ever runs over the
   candidate net the existing `WHERE` clause already narrows to, not
   the full table, so cost tracks matched rows, not table size.

7. **Random ordering reuses the old app's real seed contract, unchanged
   — client-owned, not server-owned.** Verified `crockpot/src/hooks/useSessionSeed.tsx`
   directly: a random seed persisted in `sessionStorage`, stable for one
   calendar day, regenerated only when the stored date doesn't match
   today. That contract is kept as-is (no redesign) and sent as a
   `seed` query string param. Server side: `ORDER BY md5(r.id::text ||
   $seed), r.id` when the random-ordering mode applies (case 2 below).
   `md5()` on a non-indexed expression means a real sort over the
   candidate set every time — accepted as fine at this product's scale
   (tens of thousands of rows, not millions); revisit only if that
   assumption stops holding.

8. **Three ordering modes, selected by which filters are active** — not
   a two-way scored/random split:
   - **Ingredients and/or categories selected** → scored ordering:
     `ORDER BY score DESC, created_at DESC, id`.
   - **Nothing selected at all** (no `q`, no time, no categories, no
     ingredients) → seeded-random ordering (`ORDER BY md5(r.id::text ||
     seed), r.id`). This is the actual fix for gap #1 (the original
     "just returns most-recently-created" complaint) — the plain browse
     case.
   - **Only `q` and/or time set, no categories/ingredients** → keep the
     existing plain `created_at DESC, id` default, unchanged. A
     deliberate name search shouldn't shuffle on every session; random
     ordering is the fix for "I don't know what I want," not for a
     narrow intentional search.

9. **Match-explanation fields, only populated in scored-ordering mode**
   (mode 1 above — null/zero in modes 2 and 3, since there's no signal):
   added to `RecipeCard`:
   - `matchedIngredientCount int`
   - `totalIngredientCount int`
   - `matchedCategoryCount int`
   - `score float64`
   - `tier *string` — `"best"`, `"good"`, or `null`, computed
     server-side with fixed absolute thresholds (`score >= 0.8` →
     `"best"`, `score >= 0.5` → `"good"`, else `null`). Fixed thresholds
     are viable specifically because coverage-based scoring is already
     normalized 0–1 — the old app needed relative values
     (`highestScoreInResults`/`maxPossibleScore`) only because its
     absolute-count scores had no natural ceiling.
   `CFE-021` reuses `crockpot/src/app/recipes/components/RecipeCard.tsx`'s
   `RelevanceBadge` for layout/copy only — its scoring/thresholds are
   not reused, per Non-goals.

10. **Anonymous-vs-authenticated ranking parity is automatic, not a
    design decision.** Scoring depends only on request params
    (`ingredientIds`, `categoryIds`, `q`) via the existing
    `OptionalAuthMiddleware` — nothing about the caller's identity
    (favourites, history) feeds into it. No special-casing needed.

## Data layer shape

`internal/models/recipe.go` — `RecipeCard` gains:
```go
MatchedIngredientCount int      `json:"matchedIngredientCount"`
TotalIngredientCount   int      `json:"totalIngredientCount"`
MatchedCategoryCount   int      `json:"matchedCategoryCount"`
Score                  float64  `json:"score"`
Tier                   *string  `json:"tier"`
```

`RecipeListFilter` gains:
```go
Seed string
```

`internal/sqlc/queries/recipes.sql` — `ListRecipes`/`CountRecipes` need:
- A per-recipe ingredient-total subquery/join (for `totalIngredientCount`
  and the coverage denominator).
- A per-recipe matched-ingredient-count and matched-category-count
  expression against the passed `ingredient_ids`/`include_category_ids`
  arrays.
- A `CASE`-gated `ORDER BY` (or two query variants selected in Go)
  implementing the three modes above. `CountRecipes` only needs the
  `WHERE` clause, unaffected by ordering mode.

Handler: `parseRecipeListFilter` (`recipe_requests.go`) gains a `seed`
query param, passed through unvalidated (any string is a valid seed —
it's just hashed).

## Acceptance criteria

- [ ] Selecting ingredients and/or categories scores and orders results
      by the selection-count-weighted coverage formula (Decision 4),
      verified against hand-built fixture recipes with known
      ingredient/category overlaps.
- [ ] Selecting only categories (no ingredients) still produces a
      meaningful score (`categoryCoverage` alone) — a recipe matching
      all selected categories scores 1.0 regardless of its own tag
      count.
- [ ] Selecting only ingredients (no categories) still produces a
      meaningful score (`ingredientCoverage` alone).
- [ ] `q` and time-range filters remain hard `AND` filters and do not
      affect score, order, or the match-explanation fields.
- [ ] No filters at all → results ordered by `md5(id || seed)`, stable
      across repeated requests with the same seed, and paginate without
      duplicates or gaps across multiple pages for a fixed seed.
- [ ] Only `q`/time set, no categories/ingredients → ordering is
      unchanged from current behaviour (`created_at DESC, id`).
- [ ] `matchedIngredientCount`, `totalIngredientCount`,
      `matchedCategoryCount`, `score`, `tier` are present on every
      `RecipeCard` in the scored-ordering mode, and zero/null in the
      other two modes.
- [ ] `tier` is `"best"` at score ≥ 0.8, `"good"` at score ≥ 0.5,
      `null` below — verified at the boundary values.
- [ ] `CountRecipes`/pagination totals are unaffected by ordering mode
      (same `WHERE`-only count as today).
- [ ] `requests/recipes.http` extended to cover: ingredients+categories
      scored request, categories-only, ingredients-only, no-filters
      seeded request run twice with the same seed (same order), and a
      `q`-only request (unchanged ordering).

## Non-goals

- No `is_dietary`/category-kind schema change (Decision 1) — parked as
  a future idea in `docs/specs/master-spec.md`.
- No dietary/allergen flags on recipes — separate future feature, not
  this ticket.
- No missing-ingredient penalty — a recipe isn't scored down for having
  ingredients you didn't select, only up for ones you did.
- No porting of the old app's absolute-count weights, `unstable_cache`,
  or fetch-all-and-JS-sort pagination pattern.
- No frontend work — `seed` generation/persistence, filter UI, and
  badge rendering are `CFE-020`/`CFE-021`, both blocked on this ticket,
  not part of it.
- No relative/result-set-dependent scoring (`highestScoreInResults`,
  `maxPossibleScore`) — fixed absolute thresholds replace it entirely.

## Verification modes

- **Service/API boundary** (`~/.claude/CLAUDE.md`): real requests
  against the real Neon dev DB, per piece, not batched at the end —
  `./scripts/test-repo.sh -run <TestName>` for repository-layer scoring/
  ordering tests (hand-built fixture recipes with known ingredient and
  category compositions, since correctness here depends on exact
  coverage arithmetic no mock can meaningfully stand in for), plus
  `go test ./internal/handler/...` for handler-layer parsing of the new
  `seed` param and pass-through of the new `RecipeCard` fields.
- **Limits/thresholds**: the `tier` boundary values (0.8, 0.5) exercised
  through the real repository query with fixtures scored exactly at and
  just below each boundary — not asserted synthetically in isolation
  from the SQL that computes them.
- **Manual regression**: `requests/recipes.http`, extended per the
  acceptance criteria above, run end-to-end against a running server.

## Piece order (AI-driven)

1. Schema-free groundwork: add `Seed` to `RecipeListFilter`, add the
   five new fields to `RecipeCard`, extend `parseRecipeListFilter` for
   `seed`. No scoring logic yet — stub returns zero/null. Test: handler
   parses `seed` and passes it through the filter.
2. Ingredient coverage: `totalIngredientCount`/`matchedIngredientCount`
   computed correctly per recipe against `ingredient_ids`. Repository
   test with fixtures.
3. Category coverage: `matchedCategoryCount`/`categoryCoverage` against
   `include_category_ids`. Repository test.
4. Combined score + tier: the selection-count-weighted formula and
   fixed thresholds, both axes together and each alone. Repository
   test covering all three `I`/`C` cases plus the tier boundaries.
5. Ordering modes: the three-way `CASE`-gated `ORDER BY` (scored /
   seeded-random / plain-default), including the seed's `md5()`
   expression and pagination stability across repeated requests.
   Repository test.
6. `.http` regression file update, full acceptance-criteria pass.

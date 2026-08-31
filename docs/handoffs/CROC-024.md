# CROC-024 — Data migration: users + items + recipes

**Implementation mode: AI-driven.** Claude implements one piece at a time
per `crockpot-go/CLAUDE.md`'s cadence: failing test → minimal stub that
fails for the right reason (fake sentinel values, never a panic) →
confirm red → stop for go-ahead → implement → confirm green → stop.
A migration CLI has few pure-logic units; the transform/mapping code is
genuinely test-first, the DB write path is proven by a manual full run +
count reconciliation (see Verification).

## Summary

`cmd/migrate-data` — a one-off Go CLI that imports the old
Next.js/Prisma/MongoDB app's **3 admin users, 387 items and 213
recipes** into the Postgres dev database, so the founder has a realistic
corpus to build the frontend and the CROC-042 ranking work against.
Pulled forward from its backlog position because ranking design and
tuning need real recipe shapes — ingredient counts, category spread,
real ages — that synthetic data can't provide.

Classified **expensive-to-undo** at grill time: it fixes the data shape
of the dev corpus and the contract every later migration pass
(favourites, menus, shopping lists, spam-user cleanup, prod cutover)
builds against.

Source of the old data: MongoDB Compass JSON export (array of
Extended-JSON documents), one file per collection, in a local directory
the CLI reads via `--source`. No live Mongo connection; no
`go.mongodb.org/mongo-driver` in the module graph.

Deferred to later passes: the 40 spam/scraper `User` docs, favourites,
`recipemenus`, `shoppinglists`, menu history.

## Decisions from the interview

### 1. Source: Compass JSON export on disk, 7 collections

The founder exports from Compass. The CLI reads a directory of JSON
files, one per collection (the Compass export names —
`crockpotV3.<Collection>.json` — are accepted as-is):

| File | Used for |
| --- | --- |
| `ItemCategory.json` | lookup only — old `_id` → seeded `item_categories.id`, by `name` |
| `Unit.json` | lookup only — old `_id` → seeded `units.id`, by `name` |
| `RecipeCategory.json` | lookup only — old `_id` → seeded `recipe_categories.id`, by `name` |
| `User.json` | filtered to the 2 real users → `users` rows; also old `_id` → `name` lookup |
| `Account.json` | lookup only — `providerAccountId` (Google `sub`) per user `_id`, for `users.google_id` |
| `Item.json` | **inserted** → `items` + `item_allowed_units` |
| `Recipe.json` | **inserted** → `recipes` + `recipe_ingredients` + `recipe_categories_recipes` |

The concrete export profile, cleanup checklist, and creator analysis
live in `docs/handoffs/CROC-024-data-review.md`.

Compass emits Extended JSON: ObjectIds as `{"$oid":"…"}`, dates as
`{"$date":"…"}` (ISO string — confirmed) or `{"$date":{"$numberLong":"…"}}` (millis).
The decoder handles both date forms plus a bare ISO string (relaxed
mode), and `{"$numberInt"|"$numberLong"|"$numberDouble":"…"}` numeric
wrappers. Kept to a small hand-rolled pass over `encoding/json`'s
`json.RawMessage` / `map[string]any` — no dependency.

**Rejected — live MongoDB connection.** Removes the manual export step
but puts a heavy driver permanently in `go.mod` / CI / `govulncheck` for
a tool run a handful of times, and a frozen snapshot is more
reproducible for the prod cutover anyway.

### 2. Writes via dedicated insert queries, not `repository` Create

The spec originally said "via the repository layer, so the same
validation applies." The API's `repository` Create methods can't be used
faithfully:

- `CreateRecipe` (sqlc) hard-codes `created_at`/`updated_at` to `now()`
  and derives `created_by_name` from `(SELECT name FROM users WHERE id =
  $created_by_id)`.
- `CreateItem` takes only `name` + `category_id`.
- Both generate a fresh random `id`.

Going through them would flatten every migrated recipe's `created_at` to
one instant — and `GET /recipes` sorts `created_at DESC, id`
(`recipes.sql:81`), so the whole corpus would collapse to `id` order,
and recipe age (a plausible CROC-042 ranking input) would be lost.

Also: the bulk of the corpus (189 of 213 recipes) shares one
`created_at` (2025-07-13, the seed import) — so preserving it still
matters for *relative* order (the 189 seed recipes sit below Zoe's 22
recent ones), and within the seed block the `id` tiebreak (UUIDv5)
gives a stable pseudo-random order. Stamping `now()` would flatten all
213 to one instant.

**Decision:** `cmd/migrate-data` gets its own insert queries in
`internal/sqlc/queries/migrate.sql` that take every column explicitly:

- `MigrateInsertUser` — explicit `id`, `email`, `google_id`, `name`,
  `image`, `role`, `email_verified_at`, `created_at`, `updated_at`
  (`CreateGoogleUser` can't set `role`, which must be `ADMIN` here)
- `MigrateInsertItem` — explicit `id`, `name`, `category_id`,
  `created_at`, `updated_at`
- `MigrateInsertItemAllowedUnit` — `(item_id, unit_id)`
- `MigrateInsertRecipe` — explicit `id`, all scalar columns,
  `approved`, `created_by_id`, `created_by_name` (both explicit — **not**
  the subquery), `created_at`, `updated_at`
- `MigrateInsertRecipeIngredient` — `(recipe_id, item_id, unit_id,
  quantity, position)`, `position` = source array index
- `MigrateInsertRecipeCategoryLink` — `(recipe_id, category_id)`

**The spec's real intent — migrated rows can't be structurally invalid —
is still met:**

- Every FK is enforced by Postgres automatically (`items.category_id`,
  `recipe_ingredients.item_id` / `unit_id`, `recipe_categories_recipes.category_id`).
- The one application-level rule — a recipe ingredient's `unitId`, when
  set, must be in that item's non-empty `item_allowed_units` set
  (CROC-014 decision 4) — is reapplied in the tool. The tool has every
  item's allowed set in memory (it just built it), so this is an
  in-memory check, no DB round-trip, ~5 lines. Not a call into
  `repository` (avoids exporting internals / cross-package coupling for
  a helper that small).

The master-spec "Data migration" bullet is rewritten to record this.

### 3. Stable UUIDv5 ids from the Mongo `_id`

`id = uuid.NewSHA1(ns, []byte(mongoObjectIDHex))` with a fixed project
namespace UUID (a constant in the package). Same `_id` → same Postgres
`id` on every run, so wipe-and-reload is byte-identical and the founder's
bookmarks / `.http` fixture ids survive a reload. `google/uuid` v1.6.0
(already in the tree) provides `NewSHA1`.

Applied to `users`, `items`, and `recipes`. Child/join rows
(`recipe_ingredients`, `item_allowed_units`, `recipe_categories_recipes`)
keep DB-default ids or composite PKs as the schema already defines —
nothing external references them.

### 4. Migrate 3 users, all ADMIN

The `User` export has 42 docs: 2 real (Francis, Zoe — both `ADMIN`), 40
scraper/spam signups (all `FREE`, no name, no favourites, corporate
email domains). Recipe ownership (see the data-review doc):

| old `createdById` | recipes | maps to |
| --- | --- | --- |
| `68740e93…847576` (bulk seed import, not in `User`) | 189 | synthetic **`Crockpot`** ADMIN user |
| Zoe Thexton | 22 | migrated Zoe |
| Francis Kershaw | 2 | migrated Francis |

**Migrate exactly 3 `users` rows, all `role = 'ADMIN'`:**

- **Francis** and **Zoe** — real Google logins. `id` = UUIDv5 of their
  Mongo `_id`; `google_id` = their `providerAccountId` from
  `Account.json` (provider `google`) — this is the Google `sub`, which
  `auth/google.go:27` + `repository/user.go:25` (`GetUserByGoogleID`)
  match on, so both can log into dev and land on their migrated admin
  account. `email`, `name`, `image` from the `User` doc;
  `email_verified_at` from `emailVerified` (or `created_at` if absent);
  `role = 'ADMIN'`.
- **`Crockpot`** — synthetic, owns the 189 seed recipes (founder:
  "combination of me and Zoe, feels disingenuous to make them all
  mine"). `id` = UUIDv5 of the ghost `_id` `68740e93cd88665fea847576`
  (stable, traceable). `google_id` = the literal `"seed-import"`
  (satisfies the `google_id XOR password_hash` CHECK; not a real `sub`,
  so nobody logs in as it — intended). `email` =
  `seed-import@crockpot.local`, `name` = `"Crockpot"`, `role = 'ADMIN'`.

The ghost→`Crockpot` alias is a hard-coded constant in the tool
(`ghostCreatorID = "68740e93cd88665fea847576"`), not config.

Every recipe therefore gets a non-NULL `created_by_id` and a
`created_by_name` (`"Francis Kershaw"` / `"Zoe Thexton"` / `"Crockpot"`).
`?mine=true` and future favourites/ownership work all function.

**If `Account.json` is unavailable:** fall back to `google_id =
"pending:<email>"` for Francis and Zoe and print a one-line `UPDATE`
each for the founder to run once they have the real `sub`. Not the
default — the export is a 30-second job.

**Still deferred:** the 40 spam users, favourites, `recipemenus`,
`shoppinglists`, menu history.

**Consequence resolved:** because every migrated recipe now has a real
`created_by_id`, the zero-UUID `createdById` serialisation concern from
the grill doesn't arise for this data. Still flagged for CROC-016 as a
latent issue for the genuine deleted-user case.

### 5. Reference-data name mismatch → abort before any write

The tool builds all three name maps (`itemcategories`, `units`,
`recipecategories` → seeded UUIDs) first. Match is on `name`, exact after
`strings.TrimSpace`.

Source rows with an **empty `name`** are skipped when building the map
(they can't be a seeded target) and their old `_id`s recorded as
"dropped reference" — LESSONS 2026-08-30 (CROC-011): the old `units`
export contained a junk `{name:"", abbreviation:""}` row. A junk row must
not trigger the abort below; an item/recipe that later references a
dropped `_id` is handled by decision 6 (unresolved category → skip item;
unresolved unit → null the unit).

If **any non-empty** old name has no seeded match, the tool collects
*every* miss, prints them with the seeded table's names beside each, and
exits non-zero having written nothing:

```
FATAL: 2 item-category names from Mongo have no match in seeded item_categories:
  "Store cupboard"  — seeded: Cupboard, Condiments, Herbs and Spices, …
  "Vegetables"      — seeded: Veg, …
Reconcile db/migrations/000003 (or the source data) and rerun.
```

Reference data is a tiny curated set (13 / 20 / 28 rows). A mismatch
means the seed is incomplete or the source has a variant spelling — both
reconcile deliberately (usually by editing the seed migration). The
migration is wipe-and-rerun, so aborting costs only a rerun.

**Rejected — skip the owning row and continue.** Silently produces a
partial corpus.

### 6. Row-level data policy

Import imperfections as-is; skip a row only when it can't be made
referentially sound; always report.

| Situation | Action |
| --- | --- |
| Recipe with 0 ingredients, 0 or >3 categories, very short/long name, odd time/serves, non-Cloudinary image URL | import as-is, report line |
| Recipe `createdById` = the ghost seed id | map to the synthetic `Crockpot` user (decision 4) |
| Recipe `createdById` not in `User.json` and not the ghost id | import, `created_by_id`/`created_by_name` NULL, report count (0 expected for this export) |
| Recipe missing `createdAt` | fall back to `updatedAt`, then `now()`; report count |
| Same `itemId` twice in one recipe's `ingredients` | keep first, drop rest, report (2 known: BBQ Pulled Pork, Sweet & Sour Chicken — data-review doc) |
| Item's `allowedUnitIds` has **any** entry that doesn't map to a seeded unit | drop the **whole** set → unconstrained (CROC-014 "faithful or empty, never partial"), report (0 expected — export is clean) |
| A recipe ingredient's **item** doesn't resolve to an inserted `items` row | **skip the whole recipe**, loud report |
| A recipe ingredient's **unit** is the old blank `{name:""}` unit (the pre-existing "unitless" convention — ~620 ingredients) | set `unitId: null`, count as `unit-blank` (expected, omitted from the per-line report) |
| A recipe ingredient's **unit** id resolves to nothing at all (not the blank row, genuinely absent) | set `unitId: null`, count as `unit-nulled` with per-line detail (suspicious) |
| A recipe ingredient's **unit** (when set and resolved) isn't in that item's allowed set *after decision 7's additions are applied* | **skip the whole recipe**, loud report |
| An item's **category** doesn't resolve (orphan ObjectId, not a name mismatch) | **skip the whole item**, loud report |

The run always inserts what it can, prints a summary (rows inserted per
table + every skip/adjustment with its reason), and **exits non-zero if
anything was skipped**.

**Skip-recipe over import-with-null-unit** for the unit case: surfaces
the problem instead of quietly degrading the data. This rule is checked
*after* decision 7's additions table is applied.

### 7. `allowedUnitIds` gaps: hard-coded additions table in the tool

23 ingredients across 22 recipes use a unit not in that item's
`allowedUnitIds` — the "added late, never enforced" data. Founder
reviewed the full list (data-review doc) and ruled every one a
reasonable use.

**Decision:** `cmd/migrate-data/fixups.go` holds a hard-coded
`allowedUnitAdditions map[string][]string` (item name → units to add) —
the 11 entries from the data-review doc. During item import the tool
unions these into the item's `allowedUnitIds` before writing
`item_allowed_units`, printing each: `widened "Mayonnaise": +milliliters`.
No manual Mongo edits, no re-export churn.

This is the same class of hard-coded specific as `ghostCreatorID` and the
3 synthetic/real user rows — appropriate for a one-off tool, all in
`fixups.go`. The table is idempotent (union): an entry already satisfied
is a silent no-op; an entry naming an item that no longer exists just
doesn't match.

**Anything still stray after the additions** (a unit the table doesn't
cover) → decision 6's skip-recipe + loud report, unchanged. So a
surprise in a future re-export surfaces rather than being absorbed.

**`--ignore-item-allowed-units`** stays as a separate nuclear option:
every item imports with an **empty** (unconstrained) set. Not needed for
this export; kept for a future one where the source curation is beyond
patching. If used, the admin re-curates via `PATCH /items/:id`.

### 8. Rerun = truncate-own-tables-and-reload, behind a guard

On each run, after the name maps validate (decision 5) and the guard
passes:

```
TRUNCATE
    recipe_categories_recipes, recipe_ingredients, recipe_favourites,
    recipe_menu_entries, menu_history_entries, shopping_list_items,
    item_allowed_units, recipes, items
RESTART IDENTITY;

DELETE FROM users
  WHERE id = ANY($migratedUserIDs)          -- the 3 UUIDv5s
     OR google_id = ANY($migratedGoogleIDs); -- Francis/Zoe sub + 'seed-import'
```

Every table that FKs into `recipes`/`items` is named explicitly — **no
`CASCADE`** (code-review finding) — so a table added against those
references later fails this `TRUNCATE` loudly rather than being wiped
silently. Includes `recipe_favourites` / `recipe_menu_entries` /
`menu_history_entries` / `shopping_list_items`, which are empty now but
would hold real user data once Epics 5–6 ship: a cutover rerun clears
them, and the dry-run text says so. Keep the list in sync with
`000001_init.up.sql`. The `users` `DELETE` is **targeted** — only the 3
rows this tool owns, matched by id *or* `google_id` (so a prior real dev
Google login by Francis/Zoe, which would hold a random id, is cleared
and re-inserted canonically rather than colliding on the `google_id`
UNIQUE). It CASCADE-deletes those 3 users' `refresh_tokens` /
verification / reset rows and NULLs nothing else (other users untouched).
Reference and other auth tables never touched. Raw `pool.Exec` in the
loader, not sqlc.

Consequence: if Francis/Zoe had a live dev session from a pre-migration
login, that session 401s after a run (their JWT `sub` points at the old
random id) — they re-login and land on the migrated row. Acceptable for
a dev reload.

**Guard** (`.env` points at Neon; the same binary does the prod cutover):

- `MIGRATE_ALLOW` env var must be set (any non-empty value) or the tool
  refuses, writes nothing.
- `--yes` required; without it the tool prints the target host + database
  name and the row counts it is about to destroy, then refuses.
- Running against a `DATABASE_URL` whose host doesn't look like the dev
  endpoint requires `--allow-prod` on top.

**Ordering guarantee (worst-case composition):** parse all 7 files →
build + validate the three name maps + the `Account` sub lookup →
**abort non-zero if any reference name misses, or a real user has no
Google `sub`** → check the guard → `TRUNCATE` + targeted user `DELETE` →
insert users → insert items → insert recipes → print summary → exit
(non-zero iff skips). A reference mismatch or a failed guard never
reaches the destructive step, so the DB is never left partial.

**Rejected — upsert by stable id (no wipe).** Needs per-recipe child-row
reconciliation and leaves behind recipes since deleted from Mongo. Wipe-
and-reload is the whole point of "rerunnable".

### 9. Config / wiring

- The CLI reuses `config.Load()` for `DATABASE_URL` and the same
  `pgxpool` setup as `db.InitDB` (simple-protocol exec mode for Neon's
  pooled endpoint). It does **not** run `golang-migrate` — the schema is
  assumed already migrated (the tool checks the seeded reference tables
  are populated and aborts with a clear message if not).
- `--source <dir>` (required), `--yes`, `--allow-prod`,
  `--ignore-item-allowed-units`, `--allow-missing-google-sub` (decision 4
  fallback — insert Francis/Zoe with `pending:<email>` and print the fix
  `UPDATE`s) flags via stdlib `flag`.
- Aborts before the destructive step if: a seeded reference table is
  empty; a real user (Francis/Zoe) has no `google` row in `Account.json`
  and `--allow-missing-google-sub` isn't set.
- No new entries in `config.Config` — Mongo isn't a runtime concern.

## Package layout

```
cmd/migrate-data/
  main.go        flag parsing, guard, orchestration, exit code
  ejson.go       Extended-JSON decode ($oid / $date / $number*)
  source.go      typed structs for the 7 collections + file loading
  mapping.go     name maps, objectIDToUUID (UUIDv5), Account sub lookup
  fixups.go      hard-coded specifics: ghostCreatorID, the 3 user rows,
                 allowedUnitAdditions (the 11-item table, decision 7)
  transform.go   Mongo doc → insert params; the decision-6 row policy
  load.go        TRUNCATE + insert via sqlc migrate queries
  report.go      running tallies, summary print, exit-code decision
internal/sqlc/queries/migrate.sql   the Migrate* insert queries
```

## Non-goals

- Migrating the 40 spam/scraper users — decision 4.
- Favourites, `recipemenus`, `shoppinglists`, menu history — deferred to
  a later pass (need the write endpoints from Epics 5–6).
- `verificationtokens` collection — ignored.
- Live MongoDB connection / `mongo-driver` dependency — decision 1.
- Running schema migrations — decision 9; schema assumed present.
- The prod cutover run itself — gated behind `--allow-prod`, a separate
  explicitly-approved step, not exercised by this ticket.
- A general auto-widen of allowed units — decision 7 uses a fixed
  11-entry table, not "add whatever any recipe used"; anything outside
  the table still trips decision 6's skip-recipe.
- New HTTP endpoints — none; no new `requests/*.http` file.

## Acceptance criteria

- [ ] `go run ./cmd/migrate-data --source <dir>` with `MIGRATE_ALLOW`
      set and `--yes` imports 3 `users` + `items` + `recipes` (+
      child/join rows) into the dev DB.
- [ ] `users`: exactly 3 rows, all `role = 'ADMIN'` — Francis + Zoe with
      `google_id` from `Account.json`, `Crockpot` with
      `google_id = 'seed-import'`. Francis's / Zoe's `google_id` equals
      their real Google `sub` (a real dev Google login lands on the
      migrated row, not a new one).
- [ ] Reference maps built by `name`; **any** unmatched
      category/unit/recipe-category name → all misses listed, non-zero
      exit, **nothing written** (verified by checking row counts
      unchanged).
- [ ] User / item / recipe ids are UUIDv5-derived — a second run with
      the same source produces identical ids (spot-check a handful).
- [ ] Migrated recipes carry their real `created_at` / `updated_at`
      (not the run time); every recipe has a non-NULL `created_by_id`
      (189 → `Crockpot`, 22 → Zoe, 2 → Francis) and matching
      `created_by_name`.
- [ ] Row policy (decision 6) applied: ghost `createdById` → `Crockpot`;
      duplicate ingredient de-duped (first kept — BBQ Pulled Pork, Sweet
      & Sour Chicken); empty-`name` source reference row skipped without
      aborting; (with a doctored fixture) unresolved ingredient item →
      recipe skipped + reported, unresolved category → item skipped +
      reported, unresolved unit id → `unitId` null + counted.
- [ ] `--ignore-item-allowed-units` → no `item_allowed_units` rows
      written, no recipe skipped on unit grounds.
- [ ] Summary prints rows-inserted per table + every skip/adjustment
      with a reason; process exits non-zero iff anything was skipped.
- [ ] Guard: no `MIGRATE_ALLOW` → refuse + no writes; no `--yes` →
      print target + counts + refuse; non-dev URL without `--allow-prod`
      → refuse.
- [ ] Ordering: a doctored source with one bad unit name aborts *before*
      `TRUNCATE` (dev corpus from a prior good run is still intact
      afterward).
- [ ] Per-entity reconciliation holds: `source count − reported skips =
      destination row count`, exactly, for `items`, `recipes`,
      `recipe_ingredients`, `recipe_categories_recipes`. With the
      cleaned export: 387 items, 213 recipes, 0 skips expected.
- [ ] Three recipes spot-checked field-by-field via `GET /recipes/:id`
      against Compass.
- [ ] The 11-entry `allowedUnitAdditions` table applied (each widening
      printed); 0 recipes skipped on unit grounds for this export.
- [ ] `golangci-lint` clean, `gofmt` clean, `go mod tidy -diff` clean
      (no new dependency).

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| `ejson.go`, `mapping.go`, `transform.go`, `report.go` | Logic with assertable behaviour — failing test first, no DB | `go test ./cmd/migrate-data/...` — Extended-JSON decode (`$oid`; `$date` as ISO string, as `{$numberLong}`, and bare ISO; `$number*` wrappers); `objectIDToUUID` deterministic + collision-free; name-map hit / miss-carries-name / collects-all-misses; row policy one test per row of the decision-6 table; the `allowedUnitAdditions` union (widens the set, still trips skip-recipe for a unit outside the table) + the `--ignore-item-allowed-units` flag; summary tallies + exit-code-non-zero-iff-skips |
| DB write path | Service / DB boundary — manual, real Neon dev DB, once (no automated Neon-writing test — it would truncate the shared dev corpus; the full run + reconciliation is the proof, and close-out states a green `go test ./...` does **not** cover this) | `MIGRATE_ALLOW=dev go run ./cmd/migrate-data --source ./mongo-export --yes` → then: (a) tool's own reconciliation table shows `source − skips = destination` exactly for the 4 entities; (b) 3 recipes compared field-by-field — name, time, serves, every ingredient (item name / qty / unit), categories, instructions, notes, `createdAt` — via `GET /recipes/:id` on a local server against the migrated DB vs the same doc in Compass; (c) `GET /recipes?q=…` and one `categoryId` filter return sane counts; (d) one item that had `allowedUnitIds` in Mongo → its `item_allowed_units` rows match; (e) a real Google login in dev as Francis lands on the migrated ADMIN row (`google_id` match), not a new FREE user |
| Guard / abort ordering | Limits / config — manual, real CLI | no `MIGRATE_ALLOW` → refuses, `SELECT count(*)` on `recipes`/`items` unchanged; no `--yes` → prints host+db+counts, refuses; `--source` with one extra unit name → aborts listing it, counts unchanged; non-dev URL without `--allow-prod` → refuses |
| Lint / format / deps | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff` (must show nothing — no new dependency) |
| Review | Gate | `/code-review medium main` |

No visual/screenshot mode — CLI + backend only. No interactive-by-founder
mode beyond the manual run above (there's no UI).

## Piece order (AI-driven)

1. **`internal/sqlc/queries/migrate.sql`** + `sqlc generate`. Mechanical;
   generated code isn't TDD-stubbed. Confirms the explicit-column insert
   shape compiles against the schema.
2. **`ejson.go`** — Extended-JSON decode. Test-first.
3. **`source.go` + `mapping.go` + `fixups.go`** — typed collection
   structs, file loading, name maps, `objectIDToUUID`, `Account` sub
   lookup, the collect-all-misses abort; `fixups.go` constants.
   Test-first (file loading against small fixture JSON in `testdata/`).
4. **`transform.go`** — Mongo doc → insert params (users, items,
   recipes) + the decision-6 row policy + the ghost→`Crockpot` alias +
   the `allowedUnitAdditions` union + the `--ignore-item-allowed-units`
   flag. Test-first, table-driven. The heart of the ticket.
5. **`load.go` + `report.go` + `main.go`** — TRUNCATE, insert loop,
   guard, summary, exit code. `report.go` tallies are unit-tested;
   `load.go` / `main.go` orchestration is covered by the manual full run
   (Verification row 2).
6. **Manual full run + reconciliation + spot-checks** against the dev DB.

## Code review (`/code-review medium main`)

Run after the dev migration. Four findings, all fixed and re-verified by a
second clean dev run (`source - skipped = built = in-db` for every entity):

1. **`MigrateTruncate ... CASCADE`** reached user-data tables
   (`recipe_favourites` etc.) it claimed never to touch — invisible while
   they're empty, silent data loss at a post-Epic-5/6 cutover rerun. Now
   every FK-referencing table is named explicitly, `CASCADE` dropped, and
   the dry-run text discloses the reach.
2. **Recipe category links weren't de-duped** (ingredients were), so a
   repeated `categoryId` in one source recipe would violate
   `recipe_categories_recipes` PK and roll back the whole migration. Added
   the same `seen`-map guard the ingredient path has (`duplicate-category`
   note). 0 occurrences in the current export.
3. **Dry run always exited 0** even with skips printed. Now returns
   `exitCode(res)` like the real run.
4. **Reconciliation was tautological** (`skipped := source - dest`). Now
   `skipped` is derived independently (skip-note counts + a
   per-skipped-recipe ingredient/category tally), and `load()` runs
   `SELECT count(*)` post-commit so the table shows real `in-db` numbers;
   a `built != in-db` or `source - skipped != built` mismatch forces a
   non-zero exit.

## Companion doc

`docs/handoffs/CROC-024-data-review.md` — export profile, the 11-entry
`allowedUnitAdditions` table (applied by the tool, not by hand), creator
analysis, duplicate-ingredient cases, spam-user list.

## What the founder does before the build

Export the **`Account`** collection from Compass (the one collection not
yet provided — it holds Francis's + Zoe's Google `sub`). The other 6
exports already shared are usable as-is — no edits, no re-export.

## Disposal (post prod cutover)

This is a one-off tool; it is meant to be deleted once the real data is in
prod and confirmed. It carries no runtime cost in the meantime —
`cmd/migrate-data` is a separate binary the server never imports, it adds
**zero** module dependencies, and the `Migrate*` sqlc queries are unused
functions in the server binary (dead code, mostly stripped by DCE). The
cost of keeping it is source-tree tidiness only.

**Delete when:** the prod cutover run is done *and* prod has been live and
correct for a settling period (a week, say) — not before, since a cutover
problem means fix-and-rerun.

**Deletion is one commit:**
1. `rm -rf cmd/migrate-data/`
2. `rm internal/sqlc/queries/migrate.sql` && `sqlc generate` (drops
   `internal/sqlc/migrate.sql.go`)
3. keep `docs/handoffs/CROC-024*.md` (documents a real decision, cheap) or
   move to an archive dir

Git history preserves the whole tool. If a fresh dev/staging DB ever needs
the corpus again, `pg_dump` from prod is the better source than re-running
this against Mongo.

## Master-spec changes made alongside this handoff

- **Key architecture decisions / Data migration** — rewritten: reads a
  Compass JSON export (not a live Mongo connection); writes via dedicated
  insert queries (not `repository` Create) to preserve real timestamps +
  stable ids, with the rationale; 3-user ADMIN migration (Francis, Zoe,
  synthetic `Crockpot`); guard + truncate-and-reload rerun model.
- **Epic 8 / CROC-024** — expanded from the one-line entry to the scoped
  block: 7 collections, 3-user migration, ghost→`Crockpot` alias,
  in-tool `allowedUnitAdditions` table, rerun guard, deferred entities,
  AC. Trimmed to a Done entry at close-out.

Completed 2026-08-31 (dev). Prod cutover pending, from a fresh export.

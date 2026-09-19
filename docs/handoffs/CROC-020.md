# CROC-020 — Menu history tracking

**Implementation mode: AI-driven.**

## Summary

Every add-to-menu and remove-from-menu is recorded as one dated row in a
new append-only diary table (`menu_history_events`), written in the same
transaction as the menu write. The old app's per-recipe aggregate rows
(migrated for the two real users) are kept untouched as a frozen
"pre-tracking baseline" (`menu_history_baseline`). Tracking only: no
endpoint reads history in this ticket. Its future uses are collected in
`CROC-050` (parked).

## Depth

Expensive to undo: a history table only holds what it was built to
capture, and anything not recorded from day one can't be back-filled.
Grilled at full depth, then composed against the worst case (below).

## Claims checked against source

- Old app **writes history but nothing reads it** — the only files
  mentioning it in `crockpot/src` are `data/types.ts` and
  `data/menu/menuMutations.ts` (write-only). No precedent for a consumer.
- Old write quirks (`menuMutations.ts:7-44`, `58-113`, `155-185`): first add
  stamps `lastRemovedFromMenu = now`; re-posting a recipe already on the
  menu counted as another add; removing a recipe not on the menu still
  stamped a removal.
- **Migrated `timesAddedToMenu` is probably inflated.** The only way to
  change serves on a menu recipe was re-calling `addRecipeToMenu`
  (`useMenu.tsx:15-18` toasts "serving size updated" on that path), and
  `AddToMenuButton.tsx:47-62` fires it on every Confirm — including
  Confirm with no change. The ± stepper only edits local state
  (`:184-209`). First/last dates are reliable; the count is an upper
  bound. Size of the inflation is unknown until the data is exported.
- `menu_history_entries` already exists (`000001_init.up.sql:169-177`), a
  straight copy of the Mongo aggregate, with **no unique constraint** on
  `(recipe_menu_id, recipe_id)` and `last_removed_from_menu NOT NULL`.
- Go menu writes never write history: `UpsertEntry` is a blind
  `ON CONFLICT` upsert (`repository/menu.go:88-125`, `menu.sql`
  `UpsertMenuEntry`), so it can't tell insert from update; `RemoveEntry`
  is a blind `DELETE` (`menu.go:161-195`, `RemoveMenuEntry :exec`).
- `RemoveEntry`/`ClearMenu`/`UpsertEntry` already run inside
  `Transactor.WithinTx` with the shopping-list regen
  (`handler/menu_handler.go:123-124`, `143-147`) — history writes join that
  transaction via `queriesFor(ctx, ...)`; handler/interface signatures
  don't change, so mocks don't regenerate.
- `recipe_menu_entries.created_at` (`000010`) is a real add time: set on
  insert, untouched by the upsert's `DO UPDATE` (`CROC-019.md:111-124`),
  and no menu write path existed before that ticket.
- The migrator loads seven JSON files and **not** `recipemenus`
  (`cmd/migrate-data/source.go:104-122`); `MigrateTruncate`
  (`migrate.sql:50-66`) names `menu_history_entries` explicitly and its
  comment requires any new table FK-ing recipes/items to be added.
- Schema convention for constrained strings is `TEXT ... CHECK (... IN
  (...))`, not a Postgres enum (`000001_init.up.sql:11`, `000011`).
- `TestSchemaFKColumnsAreIndexed` (`repository/schema_test.go:12-46`) keeps a
  fixed list of FK columns that must each lead an index.

## Decisions

1. **Shape: diary + frozen baseline.** New `menu_history_events`
   (append-only). Existing table renamed `menu_history_baseline` and left as
   migrated data. Stats = baseline + events.
   *Rejected:* aggregate-only (rules out insights over time; bakes counting
   quirks in permanently); events-only (migrated aggregates would need
   invented dates, corrupting cadence/seasonal insights); events + live
   aggregate (redundant, can drift). *Scale:* diary grows with activity
   (~1M rows/week at a hypothetical 100k active users, indexed per-user
   queries stay cheap); escape hatch is compacting old events into the
   baseline table — the same mechanism. *Revisit if* a live query over
   events gets slow — add a maintained summary table (additive, loses
   nothing).
2. **Row = menu, recipe, type (`add`|`remove`), timestamp.** `TEXT` +
   `CHECK`, DB-assigned `now()` (transaction time — clear-menu rows share
   one timestamp). **No serves** (founder's call: "has the user added a
   recipe, how often"; adding a nullable column later is a one-line
   migration if that changes). No `source` column until the planner exists —
   a later default of `'menu'` is accurate for every existing row.
   Hangs off `recipe_menu_id` (matches `recipe_menu_entries` /
   baseline); cheap to change.
3. **What writes a row.**

   | Action | Row |
   |---|---|
   | Add a recipe not on the menu | `add` |
   | "Add" one already on the menu (serves-only re-post) | none |
   | `PATCH /menu/entries/:recipeId` (serves) | none |
   | Remove a recipe that is on the menu | `remove` |
   | Remove one that isn't | none |
   | Clear menu | one `remove` per recipe that was on it, same timestamp |

   Detection is atomic in the same statement (insert-vs-update on the
   upsert; `DELETE ... RETURNING recipe_id` for remove/clear) so two
   simultaneous requests can't both record an `add`. Exact upsert trick
   (e.g. `xmax = 0`) confirmed against the real DB at build time, not
   assumed.
4. **Recipe deleted → its history is deleted (`ON DELETE CASCADE`).**
   Matches every other recipe FK (`000001:161-177`); a row about a recipe
   that no longer exists answers no per-recipe question and forces every
   reader to handle a missing recipe. Accepted downside: total-activity
   stats shrink retroactively when a recipe is deleted. Account deletion
   (`CROC-030`) cascades via `recipe_menus`.
5. **One-time backfill in the same migration.** One `add` event per current
   `recipe_menu_entries` row, timestamped `created_at`. Without it a later
   removal would create a `remove` with no matching `add` (breaks pairing)
   and "recipes never on a menu" would wrongly flag recipes currently on a
   menu. Loss: anything added-then-removed between 2026-09-06 and this
   ships — dev-only (prod not deployed), the founder's own test data.
6. **History writes share the menu write's transaction.** A history insert
   failure rolls back the menu write. Chosen for pairing integrity and
   precedent (shopping-list regen already does this); a failure is not an
   expected path.
7. **Migration imports history only.** `cmd/migrate-data` gains a
   `recipemenus` loader, creates a `recipe_menus` row for each migrated
   real user (Francis, Zoe — not the 40 spam users), and inserts baseline
   rows with recipe ids mapped by the existing UUIDv5 rule; rows pointing
   at recipes not in the migrated set are skipped and counted in the
   report. **Current menu entries and shopping lists are not imported** —
   new app, new database, a scrapped menu is fine (founder's call).
   *Consequence for a future entries-import:* it must **not** write diary
   `add` rows for imported entries — the baseline already covers them.
8. **Baseline counts imported as-is.** No arbitrary reduction: the error is
   per-recipe, a flat cut would look precise while being wrong, and it
   would destroy the "real recorded data" property. Any distrust of old
   counts belongs in the reader (e.g. `CROC-050`'s formulas), where it can
   be tuned. Migrator report gains a diagnostic: rows with
   `times_added_to_menu > 1` whose first/last add are < 1 day apart
   (can't be several real cooks). Reversible: the dev migrator rewrites this
   data every run, so if the diagnostic shows a real problem the import
   can be adjusted then.
9. **No history endpoint.** The consumer decides the response shape; none
   exists yet. Diary records from ship day regardless. Future uses parked
   as `CROC-050`.
10. **Baseline table cleanup.** Rename `menu_history_entries` →
    `menu_history_baseline` (+ its index), add
    `UNIQUE (recipe_menu_id, recipe_id)`. Safe: nothing writes to the table
    today. `last_removed_from_menu` stays `NOT NULL` (Mongo always sets it).
    A table comment records that `times_added_to_menu` is an upper bound.

## Composed against the worst case

- **Concurrent same-recipe adds / removes** → atomic detection (decision 3);
  tested with concurrent goroutines under `-race`, as `CROC-022` did.
- **Migrator re-run on dev** truncates and re-imports the baseline; the
  events table must be named in `MigrateTruncate` (its comment makes an
  omission fail loudly), so a re-run also wipes diary rows built through
  the new app — same hazard already true of entries/favourites. **Prod
  cutover must run into an empty DB before real users act**, or the
  truncate wipes their activity (existing `--allow-prod` guard applies).
- **Baseline vs. backfill overlap:** none — current entries aren't
  imported, so no recipe is counted in both (decision 7).
- **Prod at deploy:** no entries exist, so the backfill inserts nothing;
  first prod rows come from the migrator's baseline and then live writes.
- **Deleting a recipe or an account** cascades both history tables; no
  orphan rows, no manual cleanup.
- **A history insert fails** → the menu write and shopping-list regen roll
  back with it (decision 6).

## Non-goals

- Any read endpoint or frontend surface for history (`CROC-050`).
- Recording serves-only changes, serves values, or a `source` column.
- Importing current menu entries or shopping lists from Mongo.
- Adjusting the migrated counts.
- Enforcing add/remove alternation with a constraint.
- Planner interaction (`CROC-025` will decide how slotting relates to
  history).

## Acceptance criteria

- [x] Migration creates `menu_history_events` (`recipe_menu_id`,
      `recipe_id` FKs `ON DELETE CASCADE`, `TEXT` type with `CHECK`,
      timestamp default `now()`), with indexes so each FK column leads one;
      `TestSchemaFKColumnsAreIndexed`'s list is extended and passes.
- [x] Same migration renames the baseline table + index, adds the unique
      constraint and the table comment, and backfills one `add` per
      existing `recipe_menu_entries` row using its `created_at`; the
      `down` migration reverses all of it cleanly.
- [x] `POST /menu/entries` for a recipe not on the menu writes exactly one
      `add`; for one already on the menu (serves change) writes none.
- [x] `PATCH /menu/entries/:recipeId` writes none.
- [x] `DELETE /menu/entries/:recipeId` writes one `remove` when the recipe
      was on the menu, none when it wasn't (including no menu row at all).
- [x] `DELETE /menu` writes one `remove` per recipe that was on the menu,
      sharing a timestamp; none for an empty or missing menu.
- [x] A history write commits or rolls back with the caller's transaction
      (test: a `WithinTx` that errors after `UpsertEntry` leaves neither the
      entry nor its event). A history failure undoing the menu write holds
      structurally — each write and its event are one SQL statement — and
      can't be forced from a test.
- [x] Concurrent identical adds produce one `add` row; concurrent identical
      removes produce one `remove` row (goroutines, `-race`).
- [x] Deleting a recipe removes its events and baseline rows; deleting a
      user removes theirs. (Baseline cascade relies on `000001`'s FKs; only
      the events cascade has its own test.)
- [x] `MigrateTruncate` names `menu_history_events`; a migrator re-run
      leaves no diary rows behind.
- [x] `cmd/migrate-data` loads `recipemenus`, creates menu rows for the two
      real users only, imports baseline rows with mapped recipe ids, skips
      and counts rows with unmapped recipes, and prints the
      inflation diagnostic in its report.
- [x] Existing `internal/handler` suite and the real-DB menu / shopping-list
      / favourite tests pass unmodified.

## Verification modes

- **Service/API boundary** — real requests against the real Neon dev DB:
  `./scripts/test-repo.sh -run <TestName>` per piece for every table row
  above, the cascade, rollback and concurrency tests. Handler suite
  (`go test ./internal/handler/...`) run to confirm unchanged green.
- **Migration** — `migrate up` / `down` / `up` round-trip against the dev
  DB (the `CROC-037` pattern) plus the extended schema test.
- **Data migration** — dry-run report, then a real run against dev using
  a fresh `recipemenus` Compass export; inspect the diagnostic line and
  spot-check a known recipe's baseline row against Compass. **Manual
  step for the founder:** produce the export (follow the existing
  `crockpotV3.<Model>.json` naming; confirm the collection/file name when
  the loader is written).
- **Manual regression** — no endpoint is added or changed, so no new
  `.http` section. Run `requests/menu.http` top-to-bottom against the local
  server, then `SELECT` from `menu_history_events` on dev to confirm rows
  match the actions (history isn't readable through the API).
- **Lint/format** — `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...`, `gofmt`, `go vet`.
- Nothing visual or interactive in this ticket.

## Build order (one commit per piece, stop at each boundary)

1. **Schema + tracking**: migration (events table, baseline rename/unique/
   comment, backfill), sqlc queries, repository write hooks, tests
   red → green, `MigrateTruncate` + schema-test updates.
2. **Migrator baseline import**: `recipemenus` loader, menu rows for real
   users, baseline insert, skip/diagnostic reporting, dev run.

`/code-review medium main` once both are green, before close-out.

Completed 2026-09-19.

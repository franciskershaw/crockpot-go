# CROC-059 — Cap the menu at 30 recipes

`POST /menu/entries` refuses a recipe that isn't already on the menu once
the menu holds 30, with `409 menu_limit_reached`. A serves change to a
recipe already on the menu never counts. Grilled 2026-10-09 together with
`crockpot-react` `CFE-045` (`docs/handoffs/CFE-045.md` there). Cheap to
undo.

**Implementation mode**: AI-driven.

## Facts this rests on (checked 2026-10-09)

- The only writer of `recipe_menu_entries` is `UpsertMenuEntry`
  (`internal/sqlc/queries/menu.sql:9`), called only from
  `PostgresMenuRepository.UpsertEntry` (`internal/repository/menu.go:95`),
  called only from `MenuHandler.UpsertEntry`
  (`internal/handler/menu_handler.go:51`).
- `UpsertMenuEntry` is `INSERT … ON CONFLICT DO UPDATE SET serves`, so add
  and serves change share one statement.
- `GetOrCreateMenu` (`menu.sql:1`) is `ON CONFLICT (user_id) DO UPDATE`,
  which row-locks the user's `recipe_menus` row until commit. It runs
  before the upsert inside the handler's `WithinTx`, so menu writes per
  user are already serialised.
- Precedent: the regulars cap, `const maxRegularsPerUser = 50`
  (`regular_handler.go:12`), passed into the repo as `limit`
  (`regular_handler.go:55`), `409 regulars_limit_reached`
  (`regular_handler.go:121`). Recipe cap: `409 recipe_limit_reached`
  (`recipe_handler.go:369`).
- Planner (`CROC-025`) is a 7×3 grid; a slot's recipe must be on the menu
  or in favourites. Old-app menus hold 0 and 2 entries.

## Decisions

1. **Cap is 30, every tier.** Covers a fully-planned week of 21 distinct
   recipes with headroom; it's a load/usability guard, not a product
   tier, same call as regulars (`CROC-038` decision 4). Revisit if real
   users hit it.
2. **Separate count, not a conditional upsert.** A `WHERE count < max` on
   the upsert's `SELECT` would also suppress the `DO UPDATE` branch, so a
   serves change on a full menu would return 200 and change nothing. One
   new query returns the menu's entry count and whether this recipe is
   already on it; refuse only when absent and count ≥ limit.
3. **Hard cap, not soft.** The count runs after `GetOrCreateMenu`, under
   its row lock, so it can't go stale before the insert. One comment marks
   that the guard relies on the `DO UPDATE` lock; a concurrency test locks
   it in.
4. **Order**: visibility check → `GetOrCreateMenu` → cap check → upsert.
   An invisible recipe still 404s on a full menu.
5. **Contract**: `models.ErrMenuLimitReached` → `409 menu_limit_reached`.
   `const maxMenuEntries = 30` in `menu_handler.go`, passed to
   `MenuRepository.UpsertEntry` as a `limit int` parameter.

## Acceptance criteria

- [ ] Adding a new recipe to a menu holding the limit returns
      `ErrMenuLimitReached`; no entry and no `menu_history_events` row is
      written.
- [ ] A serves change to a recipe already on a full menu succeeds and
      updates serves.
- [ ] The entry that brings the menu to exactly the limit succeeds.
- [ ] Two concurrent adds at limit − 1 produce exactly one success; the
      test is seen failing with the lock removed.
- [ ] Handler maps `ErrMenuLimitReached` to `409 {"error":
      "menu_limit_reached"}`; an invisible recipe on a full menu still 404s.
- [ ] Mocks regenerated (`go tool mockery`) after the interface change.
- [ ] Spec endpoint docs and `requests/menu.http` note the 409.

## Non-goals

- Per-tier caps.
- Any change to `PATCH`/`DELETE /menu/entries` or `DELETE /menu`.
- A count in `GET /menu` (the client already has the entries).

## Pieces

1. **Repo** — error, count+presence query, `limit` param, mock regen,
   repo tests (criteria 1–4). Red, stop, green, stop.
2. **Handler** — const, 409 branch, handler test, spec + `.http` docs.

## Verification

- **Logic** (failing tests first): `./scripts/test-repo.sh -run
  TestUpsertEntry`, `go test ./internal/handler/...`.
- **Limits** (real client): a throwaway script against the local API
  (`lsof -iTCP:8080 -sTCP:LISTEN` first) adds 30 visible recipes, confirms
  the 31st gets `409 menu_limit_reached` and a serves change on the full
  menu gets 200, then `DELETE /menu`.
- Full repo suite green against Neon before close-out; lint uncapped.

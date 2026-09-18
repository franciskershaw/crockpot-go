# CROC-049 — Clear menu

**Implementation mode: AI-driven.**

## Summary

`DELETE /menu` removes every entry from the caller's menu in one call,
mirroring `CROC-022`'s `DELETE /shopping-list`. Surfaced as a gap the
same way that was: the design's "Clear menu" button
(`screenshots/your crockpot/yp1.png`) and the old app both have this,
but no existing ticket ever named a bulk-clear endpoint — `CROC-019`
only shipped per-entry `POST`/`PATCH`/`DELETE`.

## Decisions

1. **Triggers a shopping-list regenerate afterward, in the same
   transaction** — matches `UpsertEntry`/`UpdateEntryServes`/
   `RemoveEntry`, all three of which already regenerate as a side
   effect of every menu write. Without it, the shopping list would keep
   showing every old generated item indefinitely, since nothing else
   would trigger a recalculation.
2. **No-op-safe**: `200`, not an error, for a user with no
   `recipe_menus` row yet.
3. **No history-tracking hook.** `CROC-020` (menu history — increment/
   first/last-added, last-removed) hasn't landed. Founder's explicit
   call: ship this now without it, accept that `CROC-020` will need to
   retrofit bulk-clear to also record history when it's eventually
   built, rather than block this on that.
4. **Response**: `200 {"message": "menu cleared"}`, matching the
   sibling menu endpoints.

## Non-goals

- Menu history tracking — `CROC-020`, deferred (decision 3).

## Acceptance criteria

- [x] `DELETE /menu` removes every entry from the caller's menu in one
      call; a subsequent `GET /menu` returns empty entries.
- [x] `DELETE /menu` regenerates the shopping list in the same
      transaction — any previously-generated items with no remaining
      menu-recipe need disappear; a regen failure rolls back the menu
      clear too. (Handler wiring matches `RemoveEntry`'s already-tested
      pattern; regen's own item-removal behavior already covered by
      `CROC-021`'s `TestRegenerate_RemovesItemNoLongerNeeded`.)
- [x] `DELETE /menu` for a user with no `recipe_menus` row returns
      `200`, not an error, and creates no row.
- [x] `DELETE /menu` only clears the caller's own menu, never another
      user's.
- [x] `requests/menu.http` extended to cover clear-menu end-to-end.

## Verification modes

- **Service/API boundary**: real requests against the real Neon dev DB
  — `./scripts/test-repo.sh -run <TestName>` for the repository-layer
  clear + regen interaction; `go test ./internal/handler/...` for
  handler-layer wiring.
- **Manual regression**: extend `requests/menu.http`.

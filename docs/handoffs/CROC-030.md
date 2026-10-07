# CROC-030 — Self-service account deletion (Epic 12: Account Management)

`DELETE /me` permanently deletes the caller's account. Their own data goes
with it, their unapproved recipes are deleted, and their approved recipes stay
as community recipes without a name. AI-driven. Grilled 2026-10-07 with
`CROC-051` and `crockpot-react` `CFE-055`. Build after `CROC-051`: the
byline scrub depends on its live name lookup.

## Current state (read 2026-10-07)

- Every table a user owns references `users` with `ON DELETE CASCADE`:
  `refresh_tokens`, `email_verification_tokens`, `password_reset_tokens`,
  `recipe_favourites`, `recipe_menus` (and through it menu entries and
  history), `shopping_lists` (and their items), and `regular_items`
  (`000001_init.up.sql`, `000014_regular_items.up.sql`). No planner table
  exists yet.
- `recipes.created_by_id` is `ON DELETE SET NULL` (`000001_init.up.sql:118`),
  and `recipe_visible_to` never matches a null creator
  (`000015_recipe_visible_to.up.sql`), so unapproved recipes left behind
  would be visible only to admins.
- Recipe delete (`internal/repository/recipe.go:471`) locks the recipe
  (`GetRecipeForWrite … FOR UPDATE`), reads which menus hold it, deletes it,
  and reports an orphaned photo. The handler rebuilds those users' shopping
  lists in the same transaction and destroys the photo after commit
  (`recipe_handler.go:167-195`).
- Access tokens aren't checked against the database (`internal/middleware/auth.go`);
  `GET /me` returns 401 for a deleted user (`CROC-009`, decision 3).

## Decisions

1. **Hard delete:** `DELETE FROM users` in one transaction, with no grace
   period and no undo. The cascades already exist, the spec has no
   soft-delete need, and soft delete would put a filter on every user query
   and keep the email address unavailable for a new sign-up.
2. **Lock the user row first** (`SELECT … FROM users WHERE id = $1 FOR
   UPDATE`). A concurrent insert that references the user (recipe,
   favourite, menu entry) takes `KEY SHARE` on that row, so it waits, then
   fails its foreign-key check after commit. Without the lock, a recipe
   created between steps 3 and 4 would survive as an unapproved orphan.
3. **Delete the user's unapproved recipes** in the same transaction, using
   the recipe-delete internals (read which menus hold each one, delete it,
   rebuild those shopping lists, collect orphaned photos). The permission is
   "the account owner deleting their own drafts", not `recipeWriteError`, so
   this path calls the repository internals, not the public handler.
   Unapproved recipes are private drafts (`CROC-062`: only approved recipes
   belong to the community). Leaving them behind would let an admin publish
   content whose author had asked for it to be removed.
4. **Approved recipes stay** with `created_by_id` set to null. Their byline
   disappears because `CROC-051` makes it a live lookup.
5. **Orphaned photos are destroyed after commit**, best-effort (logged, never
   returned), the same as recipe delete.
6. **Proof of identity:** password accounts must send the current password,
   checked with bcrypt; a wrong one returns 403 `invalid_password` (not 401,
   for the reason in `CROC-051` decision 7). Google accounts send nothing
   extra. The frontend's typed-email phrase only guards against misclicks.
   Accepted risk: someone at a Google user's unlocked, signed-in device can
   delete the account.
7. **ADMIN can't delete their own account:** 403 `admin_cannot_self_delete`.
   There's one admin and roles are granted by hand, so an admin is removed
   by demoting them in SQL first.
8. **The 15-minute window is accepted.** Another device's access token keeps
   working until it expires. Writes then fail their foreign-key check (a
   500), so no data comes back, and the next refresh fails, which signs that
   device out (`CFE-050`). No per-request database check, no deny-list.
9. **No "account deleted" email.** After a hard delete there's nothing left
   for the user to act on.
10. **Rate limit:** the 10/min auth limit, because the endpoint checks a
    password.

## API

| Endpoint | Body | Success | Errors |
|---|---|---|---|
| `DELETE /me` | `{password}` (password accounts; ignored for Google) | 204, refresh cookie cleared | 403 `invalid_password`; 403 `admin_cannot_self_delete` |

## Acceptance criteria

- [ ] After a successful delete, no rows remain for the user in any cascaded
      table, and the `users` row is gone.
- [ ] The user's unapproved recipes are gone. An admin whose menu held one
      gets a rebuilt shopping list without it. Each orphaned photo has its
      destroy attempted.
- [ ] Their approved recipes still exist with `created_by_id` null; detail
      returns `createdByName: null`; other users' favourites and menu entries
      for them are untouched.
- [ ] A recipe insert for the user that starts while the delete holds the
      row lock fails after the commit and leaves no recipe behind (a
      deterministic lock-wait test, following `CROC-058`'s
      `waitForLockWait`, seen failing without the lock).
- [ ] A wrong password returns 403 `invalid_password` with nothing deleted;
      a Google account deletes without a password.
- [ ] An ADMIN gets 403 `admin_cannot_self_delete` with nothing deleted.
- [ ] The response clears the refresh cookie; refreshing with the old
      cookie afterwards returns 401.
- [ ] The 11th call within a minute returns 429.
- [ ] `requests/me.http` gains a `DELETE /me` section that registers and
      deletes its own throwaway account.

## Non-goals

- A grace period, undo, or soft delete.
- Re-authenticating Google accounts (a fresh Google sign-in or an emailed
  code).
- An admin deleting other users.
- Planner rows: none exist yet. Whichever ticket builds `CROC-025` must give
  them `ON DELETE CASCADE`.

## Verification

- **Logic, test first:** repository tests against Neon
  (`./scripts/test-repo.sh -run TestDeleteAccount`) covering cascades,
  unapproved vs approved recipes, the admin's rebuilt list, and the lock
  test; handler tests (`go test ./internal/handler/...`) covering the
  password and admin checks, the cookie clearing, and the order of photo
  destruction.
- **Service boundary:** `requests/me.http` against a local server: register,
  confirm, create a recipe, delete, then `GET /me` and refresh with the old
  token and cookie (both 401). This also proves a `DELETE` with a JSON body
  reaches Gin.
- **Limits:** 11 `DELETE /me` calls with a wrong password from REST Client;
  the 11th returns 429.

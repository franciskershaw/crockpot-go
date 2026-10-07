# CROC-051 — Profile and password (Epic 12: Account Management)

Users can edit their display name (`PATCH /me`) and, on password accounts,
change their password (`POST /me/password`). The byline on a recipe becomes a
live lookup of the creator's name. A returning Google sign-in stops
overwriting the profile, and `users.image` is dropped (absorbs `CROC-044`).
AI-driven. Grilled 2026-10-07 with `CROC-030` and `crockpot-react` `CFE-055`
as one epic. Build this before `CROC-030`, which relies on the live byline.

## Current state (read 2026-10-07)

- `GetOrCreateUser` (`internal/repository/user.go:22`) calls
  `refreshLoginProfile` on every returning Google sign-in, which overwrites
  `name` and `image` with Google's (`users.sql:29-36`, `UpdateUserLoginProfile`).
  This was `CROC-004`'s choice, made so the stored profile tracked Google's.
- `recipes.created_by_name` is copied from `users.name` once, at create
  (`recipes.sql:26`, a subquery) and in `cmd/migrate-data`
  (`transform.go:509-521`, which takes the migrated user's name). Every stored
  value equals the creator's `users.name`, and none exist without a
  `created_by_id`. It is read only by `buildRecipeDetail`
  (`internal/repository/recipe.go:307`).
- `CROC-002` added `created_by_name` as a permanent snapshot, so attribution
  would survive the creator's deletion and renames
  (`docs/handoffs/CROC-002.md:88-101`).
- `users.image` is unread: `crockpot-react` shows initials only (since
  `CFE-004`).
- Register checks `name` only for presence (`binding:"required"`,
  `auth_handler.go:246`): no trim, no maximum.
- Password length rules (`minPasswordLength` = 8, `maxPasswordBytes`) are
  written out twice: register (`auth_handler.go:255-260`) and reset
  (`629-634`).
- `/auth/*` runs under a 10/min limit (`main.go:40,132`). The signed-in
  group gets only the global 120/min.
- `crockpot-react`'s `apiFetch` treats any 401 on a signed-in request as an
  expired session and refreshes (`CFE-050`).

## Decisions

1. **A Google name is set when the account is created, never after.** A
   returning Google sign-in updates only `last_login_at`. Reverses `CROC-004`:
   keeping the stored name in step with Google made sense only while nothing
   could edit it. Now it would undo the user's own edit.
2. **Drop `users.image`** (absorbs `CROC-044`): the column (migration), the
   `models.User` field, the `/me` field, `IDTokenClaims.AvatarURL` capture, and
   the `cmd/migrate-data` copy-through. Nothing reads it. The app isn't
   deployed, so no live data is lost.
3. **The byline is a live lookup.** Drop `recipes.created_by_name`, and have
   `buildRecipeDetail` read the creator's current `users.name` (a join or one
   lookup). A rename then shows on every recipe the user created. After
   `CROC-030`'s delete, `created_by_id` is null (`ON DELETE SET NULL`), so the
   byline goes with no extra step. Reverses `CROC-002`'s snapshot: it was added
   to keep attribution after deletion, but the founder now wants a deleted
   user's name gone. That matches how content sites treat deleted authors
   (GitHub's "ghost", Reddit's "[deleted]"). The frontend already hides the
   byline when `createdByName` is null (`RecipeHero.tsx:107`). The API field
   name and shape don't change.
4. **Name rule: trimmed, 1–50 characters**, otherwise 400 `invalid_name`.
   Applied to `PATCH /me` and to register. A name can't be cleared.
5. **`/me` gains `authProvider`: `"password" | "google"`**, derived from
   which of `google_id`/`password_hash` is set (the DB `CHECK` makes them
   mutually exclusive). The allowlist becomes `id, email, name, role,
   authProvider` (`image` removed, per 2).
6. **`POST /me/password` revokes every refresh family, then issues a fresh
   session** to the caller: `RevokeAllRefreshTokenFamiliesForUser`, then
   `issueRefreshSession`, with the same response shape as login. People
   change their password because they think someone else has it, so other
   sessions must die (as `CROC-007`'s reset does), and the user who just
   proved their password stays signed in. Other devices keep the 15-minute
   access-token window, as already accepted for role changes.
7. **A wrong current password returns 403 `invalid_password`, not 401.** On a
   signed-in request, a 401 makes the frontend refresh and retry the session.
   `refreshOn401: false` is meant for requests made before a session exists.
8. **A "your password was changed" email**, sent after commit and
   best-effort: a send failure is logged and never fails the request (the
   same pattern as `destroyImage`). It's the only signal the owner gets if
   someone else made the change.
9. **The password length rules move into one helper**, used by register,
   reset and change.
10. **Rate limit:** `POST /me/password` gets the 10/min auth limit, because a
    stolen session must not be able to try 120 passwords a minute.

## API

| Endpoint | Body | Success | Errors |
|---|---|---|---|
| `PATCH /me` | `{name}` | 200, `/me` shape | 400 `invalid_name` |
| `POST /me/password` | `{currentPassword, newPassword}` | 200 `{accessToken}` + new refresh cookie | 403 `invalid_password`; 409 `no_password` (Google account); 400 `password_too_short` / `password_too_long` |

## Acceptance criteria

- [ ] A returning Google sign-in leaves `name` unchanged when the Google
      claim differs; it still updates `last_login_at`.
- [ ] `users.image` is gone: migration up/down, model, queries,
      `/me`, the Google claim, and `cmd/migrate-data` all updated, and
      `go build ./...` is clean, `cmd/migrate-data` included.
- [ ] `recipes.created_by_name` is dropped. Recipe detail returns the
      creator's current name; after a rename, the next detail read shows the
      new name. A recipe with a null `created_by_id` returns
      `createdByName: null`.
- [ ] `PATCH /me` trims; `""`, whitespace only, and 51+ characters return
      400 `invalid_name`; 50 characters succeeds. Register applies the same
      rule.
- [ ] `/me` returns `authProvider` correctly for both account kinds and no
      `image`.
- [ ] `POST /me/password`: the correct current password changes the hash,
      revokes every earlier refresh family (an old cookie's refresh then
      fails), and returns a working access token plus a new cookie; a wrong
      current password returns 403 `invalid_password` with nothing changed; a
      Google account returns 409 `no_password`; a short or long new password
      returns the existing codes.
- [ ] The change email is sent once on success; a forced send failure still
      returns 200.
- [ ] The 11th `POST /me/password` within a minute from one client returns
      429.
- [ ] `requests/me.http` gains `PATCH /me` and `POST /me/password`
      sections, plus cleanup.

## Non-goals

- Changing email (nobody needs it yet, and it would depend on `CROC-069`'s
  case-insensitive emails).
- A session list or "sign out everywhere".
- Turning foreign-key failures in the post-delete token window into 401s.

## Verification

- **Logic, test first:** handler tests with mockery mocks
  (`go test ./internal/handler/...`) for validation, status codes and the
  revoke-then-issue order; repository tests against Neon
  (`./scripts/test-repo.sh -run 'TestUpdateName|TestGetOrCreateUser|TestRecipeDetail'`)
  for the live byline, the unchanged name on Google sign-in, and the
  migration's column drops (`migrate down 1` / `up` against dev).
- **Service boundary:** real requests through `requests/me.http` against a
  local server: rename then fetch a recipe's detail; change password, then
  refresh with the old cookie (expect 401).
- **Email:** one real change against a dev account, with the email checked
  in the inbox.
- **Limits:** fire 11 `POST /me/password` from REST Client and see the 429.

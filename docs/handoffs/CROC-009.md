# CROC-009 — GET /me profile endpoint

**Implementation mode: AI-driven.** Claude implements one piece at a time
per `crockpot-go/CLAUDE.md`'s AI-driven cadence: failing test → minimal
stub that fails for the right reason → confirm red → stop for go-ahead →
implement → confirm green → stop before the next piece.

## Summary

`GET /me` — the authenticated user's own profile (id, email, name, image,
role). First route to actually sit behind `middleware.AuthMiddleware`
(`CROC-003` built it, but added no protected route of its own to prove it
against — see `docs/handoffs/CROC-003.md`), so this ticket also owns
proving that middleware works end-to-end against a live request, not just
its own unit tests.

Adds `Me()` to the existing `AuthHandler` (no new handler struct — it
already has `userRepo` injected, and `UserRepository.FindByID` already
exists, unchanged since `CROC-008`). Adds the first `authed :=
server.Group("/")` route group in `main.go`, matching
`packing-list-go/main.go:119-122`'s precedent exactly — every future
protected route (Epic 3 onward) joins this same group.

**Also fixes a real bug found in this ticket's own grill, not new scope**:
`RefreshToken` (`CROC-008`) treats *any* `UserRepository.FindByID` error,
including `models.ErrUserNotFound`, as a generic `500 server_error`. That
was never a deliberate decision — `docs/handoffs/CROC-008.md` reasons about
lookup *ordering* (fail before rotating) but never about which status code
a missing user should produce; the code just fell into the same
catch-all `if err != nil { 500 }` block used for unexpected errors. See
"Decisions from the interview" below for why 401 is actually correct here,
in both `Me` and `RefreshToken`.

## Decisions from the interview

- **Route group**: `authed := server.Group("/")` +
  `middleware.AuthMiddleware(cfg.JWTSecretAccess)`, sitting alongside the
  existing `/auth`, `authTight`, `authRefresh` groups in `main.go`.
  Verified against `packing-list-go/main.go:119-141` — the only pattern
  with real architectural-reference precedent, and the one every future
  protected route needs regardless.
- **Handler home**: `Me()` on `AuthHandler`, not a new `UserHandler`.
  Matches `packing-list-go/internal/handler/auth_handler.go:268`'s own
  `Me` method. No interface changes — `FindByID` already exists on
  `UserRepository` (`internal/handler/auth_handler.go:37`), so no
  `go tool mockery` re-run needed this ticket.
- **Missing-user status code — 401, not 500, and this changes
  `RefreshToken` too.** Both `Me` and `RefreshToken` now special-case
  `errors.Is(err, models.ErrUserNotFound)` → `401 {"error":
  "unauthorized"}`, before falling through to the existing generic `500
  server_error` for any other repository error. Reasoning (from the
  interview, not just carried over from precedent):
  - 401 means "these credentials don't currently establish who you are."
    A syntactically-valid, unexpired access token naming a user who no
    longer exists is exactly that failure — the same class
    `AuthMiddleware` already returns 401 for (missing/malformed/expired
    token), just caught one layer later, after a DB round-trip instead of
    before one. Splitting these into different status codes based on
    *where* the check happens rather than *what* the failure means is
    the actual inconsistency.
  - 500 should mean the server broke unexpectedly. Nothing broke; the DB
    correctly reported the row is gone. Masking that as 500 is a category
    error.
  - The enumeration-defense reasoning behind this codebase's other
    generic-error collapses (`CROC-005`'s register/login codes) doesn't
    transfer here: that guards an *unauthenticated* caller from learning
    whether an account exists. The caller here already possesses a
    validly-signed token naming that exact user ID — confirming the
    identity is no longer valid discloses nothing new.
  - No enumeration risk either way in the response body: stays generic
    (`"unauthorized"`, not `"user_not_found"`) — no product surface needs
    to distinguish "bad token" from "token's user is gone," both mean
    "log in again," matching `CROC-008`'s own single-generic-401
    reasoning for `invalid_refresh_token`.
  - There is currently no feature that deletes a user row through the
    product itself (no account-deletion epic existed before this ticket
    — see below), so today this only matters for a row removed by hand
    (e.g. `psql` cleanup) while a live token for it still exists. Revisit
    if the fix ever needs to change once account deletion actually ships.
- **Response shape: explicit `gin.H{"id", "email", "name", "image",
  "role"}` allowlist**, not marshaling `*models.User` directly. Matches
  `packing-list-go`'s own `Me()`. Defense-in-depth over the alternative:
  marshaling the struct ties `/me`'s public contract to every future
  `json:"-"` tag on `models.User` staying correct forever, and would
  silently add `createdAt` beyond the five fields the ticket names.
- **Shared `testutil.AuthHeader(t, email, userID, role string) string`
  helper added** (mirrors `packing-list-go/internal/testutil`'s own),
  since this is the first handler test needing a valid bearer token —
  every future protected-route test needs the same thing, and hand-rolling
  it per test file (as `middleware/auth_test.go`'s local
  `expiredAccessToken` does for the *invalid*-token case) doesn't scale
  past one file.
- **`.http` coverage lands in `requests/auth.http`**, not a new
  `requests/me.http`, matching `packing-list-go/requests/auth.http`'s own
  placement of `GET /me` — it's part of the same
  token-acquisition-and-verification flow file, chaining off the same
  `Login`-captured access token, despite the route not being under
  `/auth`.
- **New ticket added to the backlog: self-service account deletion**
  (see master-spec addition below). Raised during this ticket's grill
  while reasoning about the missing-user edge case — there is currently
  no way for a user to delete their own account anywhere in the spec, not
  even as a stated non-goal. Design and implementation are out of scope
  for CROC-009; this ticket only adds the backlog entry.

## Acceptance criteria

- [ ] `AuthHandler.Me(c *gin.Context)`: reads `userID` set by
      `AuthMiddleware` from context; missing (defensive — unreachable
      while `Me` only ever sits behind `AuthMiddleware`, but not
      unreachable by construction) → `401 {"error": "unauthorized"}`
- [ ] Calls `userRepo.FindByID(ctx, userID)`;
      `errors.Is(err, models.ErrUserNotFound)` → `401 {"error":
      "unauthorized"}`; any other error → `500 {"error": "server_error"}`
      (logged via `c.Error`, matching existing handler convention)
- [ ] Success → `200 {"id", "email", "name", "image", "role"}`, sourced
      entirely from the freshly-fetched `models.User` row (not mixed with
      JWT claims — claims carry no `name`/`image` at all, and the DB row
      is the single source of truth for everything returned here)
- [ ] `RefreshToken` gets the same `ErrUserNotFound` → `401 {"error":
      "unauthorized"}` branch ahead of its existing generic `500`
      fallback; existing `TestRefreshToken_FailsBeforeRotatingWhenUserLookupFails`
      (asserts 500 for a generic `"db exploded"` error) stays green,
      unchanged
- [ ] `main.go`: new `authed := server.Group("/")` +
      `middleware.AuthMiddleware(cfg.JWTSecretAccess)`; `GET /me` is its
      first member
- [ ] `testutil.AuthHeader(t *testing.T, email, userID, role string)
      string` added, generating a real signed access token via
      `auth.GenerateAccessToken`
- [ ] `requests/auth.http` extended: a `GET /me` section with a ✅ 200
      case chained off `Login`'s captured token, a 🔒 401 case with no
      token, a 🔒 401 case with a malformed token, and a 🔒 401 case
      where the user row is deleted via a manual `psql`/SQL statement
      after capturing a valid token (mirrors the file's existing pattern
      of manual SQL for edge cases it can't trigger live, e.g. the
      expired-reset-token case)
- [ ] `docs/specs/master-spec.md`: Epic 2 marked complete; new backlog
      epic/ticket added for self-service account deletion (design +
      implementation, its own future grill — not built here)

## Non-goals

- No account-deletion implementation — only the backlog entry for a
  future ticket
- No change to `AuthMiddleware` itself, or to access-token claims/expiry
- No new migration, no new interface methods, no `go tool mockery`
  re-run — `FindByID` already exists
- No repository-layer test changes — `FindByID`'s repository coverage
  (`TestFindByID_ReturnsUser`, `TestFindByID_ReturnsErrUserNotFound`)
  already exists from `CROC-008`
- No change to `RefreshToken`'s behaviour beyond the status-code fix for
  the `ErrUserNotFound` case — its rotation/reuse-detection logic is
  untouched

## Verification

- **Handler tests (no DB)**: `go test ./internal/handler/...` —
  new `TestMe_ReturnsProfile`, `TestMe_ReturnsUnauthorizedWhenUserIDMissingFromContext`,
  `TestMe_ReturnsUnauthorizedWhenUserNotFound`, `TestMe_ReturnsServerErrorOnOtherRepositoryError`;
  new `TestRefreshToken_ReturnsUnauthorizedWhenUserNotFound` alongside the
  existing (unchanged) `TestRefreshToken_FailsBeforeRotatingWhenUserLookupFails`.
- **A real request against the real dependency**: `go run main.go`, then
  run `requests/auth.http`'s new `GET /me` sections top-to-bottom against
  the live server and real Neon dev DB — this is the actual proof that
  `AuthMiddleware` (`CROC-003`) works end-to-end, which no test to date
  has exercised against a live request. Includes the manual-SQL-delete
  401 case described above.
- **Lint/format/build**, same as every prior ticket: `gofmt -l .`,
  `go vet ./...`, `go build ./...`,
  `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`,
  `go mod tidy -diff` (no changes expected — no new dependency).
- Per-ticket review is CodeRabbit's, via PR, per `CLAUDE.md`.

## Expected test files

- `internal/handler/auth_handler_test.go` — extended:
  `TestMe_ReturnsProfile`, `TestMe_ReturnsUnauthorizedWhenUserIDMissingFromContext`,
  `TestMe_ReturnsUnauthorizedWhenUserNotFound`,
  `TestMe_ReturnsServerErrorOnOtherRepositoryError`,
  `TestRefreshToken_ReturnsUnauthorizedWhenUserNotFound`
- `internal/testutil/fixtures.go` — extended: `AuthHeader` helper

## Close-out

Pending implementation.

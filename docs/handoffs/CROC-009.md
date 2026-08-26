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
- **`testutil.AuthHeader(t, email, userID, role string) string` added**,
  matching the original plan (mirrors `packing-list-go/internal/testutil`'s
  own) — reached via a brief detour. First attempt hit a real
  `go vet`-confirmed import cycle: `internal/auth/jwt_test.go` was
  `package auth` (white-box) and already imported `testutil` for its
  constants, so `testutil` importing `auth` back (for
  `auth.GenerateAccessToken`) cycled. `packing-list-go` never hits this
  because its own `auth` package's tests don't import its `testutil`.
  Root-caused rather than routed around: `jwt_test.go` was the *only*
  `internal/auth` test file importing `testutil` (`token_test.go`,
  `google_test.go` don't), and every symbol it uses from `auth`
  (`GenerateAccessToken`, `GenerateRefreshToken`, `ValidateAccessToken`,
  `ValidateRefreshToken`, `CustomClaims`, `RefreshClaims`) is already
  exported — so it converts to black-box `package auth_test` with a
  purely mechanical change (package clause + `auth.` prefixes, no logic
  change), which removes it from `auth`'s own compilation unit entirely.
  Go allows white-box and black-box test files to coexist in one
  directory, so `google_test.go` (which does need one unexported
  constructor, `newGoogleOAuthManager`) stays untouched as `package auth`.
  Verified before and after: `go test ./internal/auth/...` produces the
  identical 25 pass/fail result lines either way. Rejected: a new
  `internal/testutil/authtoken` subpackage, or scoping the helper inside
  `internal/handler` — both work, but only route around the cycle rather
  than fix it, and `testutil` stays the one real home for shared test
  helpers (per the folder layout) instead of splitting across
  per-avoided-cycle subpackages as more cross-package needs show up
  later.
- **`.http` coverage lands in its own `requests/me.http`, not
  `requests/auth.http`.** Reversed after implementation: originally
  recorded as landing in `auth.http` (matching `packing-list-go`'s own
  placement), but the founder found hunting through `auth.http`'s ~500
  lines to find where to fill in credentials and grab a token too much
  friction in practice. This also surfaces a real technical reason, not
  just ergonomics: REST Client scopes captured variables and cookies per
  file, so a `GET /me` section living in a separate file could never
  have chained off `auth.http`'s `Login` anyway — it needs its own
  `Login` at the top regardless of which file it's in. `me.http` reuses
  the same confirmed password account `auth.http` sets up (documented as
  a prerequisite) rather than registering a second throwaway account.
  `requests/README.md` gets a new note on this per-file variable/cookie
  scoping, since every future protected-route `.http` file hits the same
  constraint.
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
- [ ] `internal/testutil.AuthHeader(t *testing.T, email, userID, role
      string) string` added, generating a real signed access token via
      `auth.GenerateAccessToken`; `internal/auth/jwt_test.go` converted to
      black-box `package auth_test` to allow it (mechanical change only,
      identical test results before/after)
- [ ] `requests/me.http` (new file): its own `Login` at the top (same
      shared password account `auth.http` sets up), then a `GET /me`
      section (✅ 200, 🔒 401 no token, 🔒 401 malformed token). The
      valid-token-but-deleted-user 401 case rides this file's own
      `Cleanup` section: re-run `Login` once more immediately before
      `DELETE FROM users` (fresh `@authToken`, well inside its 15-minute
      expiry), delete, then one more `GET /me` with that same token
      right after — a 401 there is unambiguous proof of the
      `ErrUserNotFound` path, not ordinary expiry. `requests/README.md`
      updated with a note on why protected-route `.http` files need
      their own `Login` (variables/cookies don't cross files)
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
  run `requests/me.http` top-to-bottom against the live server and real
  Neon dev DB — this is the actual proof that `AuthMiddleware`
  (`CROC-003`) works end-to-end, which no test to date
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
- `internal/auth/jwt_test.go` — converted to `package auth_test`
  (mechanical only)

## Close-out

Implementation done (TDD: red confirmed for all 5 new tests before real
bodies existed, all green after). `go build`, `go vet`, `gofmt -l .`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
(0 issues), `go mod tidy -diff` (no changes — no new dependency) all
clean. Manual `requests/me.http` run against a live server + real Neon
dev DB confirmed. PR reviewed by CodeRabbit — one comment (about not
marking the epic done), deliberately dismissed since review was still in
progress at the time.

Completed 2026-08-26.

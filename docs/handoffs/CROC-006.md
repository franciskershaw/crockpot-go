# CROC-006 — Email/password login

Grilled 2026-08-16.

**Implementation mode: hand-written.** Founder implements against this
doc; Claude writes no code, reviews the diff after (`code-review`
available on request — see `crockpot-go/CLAUDE.md`'s "Overrides"). Every
piece this ticket needs has a direct precedent inside
`internal/handler/auth_handler.go` itself (`bcrypt.CompareHashAndPassword`
mirrors `Register`'s already-used `GenerateFromPassword`; the
token-issuing tail is a straight reuse of `GoogleCallback`'s; the
typed-error → JSON-code mapping matches `Register`/`ConfirmEmail`) — no
genuinely new stack surface like `CROC-005`'s Resend integration.

## Context

`POST /auth/login` is the third of five Epic 2 tickets
(`CROC-005` → **006** → `007` → `008` → `009`), sequenced per the
correction at `CROC-005`'s grill (`docs/handoffs/CROC-005.md`) — Epic 2
completes in full before any `crockpot-react` work starts.

Verified before designing anything: `packing-list-go` (the architectural
reference) has **no password login precedent at all** — it's
Google-OAuth-only (`packing-list-go/internal/handler/auth_handler.go`
has `LoginWithGoogle`, `GoogleCallback`, `RefreshToken`, `Logout`, `Me`;
no `Login`). The two real precedents this design draws from instead:
`packing-list-go/internal/handler/auth_handler.go:248` — `POST
/auth/refresh` returns `{"accessToken": ...}` only, no user object, `Me`
(line 268) is the sole profile source — and this repo's own
`GoogleCallback` (`internal/handler/auth_handler.go:127-208`) for the
token-issuing tail.

## Decisions and why

- **Response is `{"accessToken": "..."}` only, no user object.** Matches
  `packing-list-go`'s `RefreshToken` precedent above. Doesn't cost a real
  round-trip in practice: `crockpot-react`'s spec
  (`docs/specs/master-spec.md:112`) already fetches `/me` on every page
  load regardless (access token lives in memory, not localStorage, so
  every load starts from zero). Keeps `/me` (`CROC-009`) the single place
  that defines the user JSON shape — login's copy would be exercised far
  less often and more likely to silently drift.
- **Failure-case check order: user lookup → password compare → confirmed
  check.** Password-mismatch is checked *before* `EmailVerifiedAt`,
  deliberately. If the confirmed-check ran first, anyone who knows a
  registered email (already disclosed via `Register`'s own `409`, see
  `CROC-005`) could learn whether that account is still unconfirmed by
  sending *any* password at all — a free, password-independent probe.
  Checking password first means `email_not_confirmed` only ever reaches
  someone who already proved they know the password. Same fix shape as
  `CROC-005`'s `ConfirmEmail` collapsing unknown-email into the same
  `code_invalid` as a wrong code.
- **Google-only account (`PasswordHash` nil) gets a distinct
  `google_account_no_password` error**, checked immediately after the
  not-found check (there's no password to compare against, so this
  can't slot into the password-first ordering above the same way).
  Not folded into generic `invalid_credentials`: `Register`'s own `409`
  already discloses this exact fact today
  (`models.ErrEmailRegisteredWithGoogle` →
  `email_registered_with_google`, `auth_handler.go:243`) — `CROC-004`
  explicitly chose to surface this collision rather than dead-end the
  user. Hiding it at login buys no security (already public via
  register) while leaving a real Google user stuck guessing why a
  password login never works for them.
- **No account-level lockout for repeated failed logins.** `ConfirmEmail`
  has a 5-attempt lockout because a 6-digit OTP (1,000,000 values) is
  brute-forceable within realistic rate-limit windows — a documented
  reason (`CROC-005` handoff), not a default to port forward. A
  bcrypt-hashed password's keyspace makes the same attack impractical
  even without one; the existing 10 req/min/IP tier (`authTight` group,
  already wired in `main.go`) is real protection against
  credential-stuffing sweeps. Matches this project's repeated
  first-principles posture (minimal v1 numbers, revisit only if real
  signups show a gap) rather than porting `ConfirmEmail`'s mechanism
  onto a case with a different threat profile.
- **No constant-time/dummy-hash mitigation for the unknown-email timing
  gap.** `Register`'s `409` already discloses whether an email is
  registered, so a timing side-channel on login reveals nothing not
  already public through a different door.
- **`users.last_login_at` is updated on successful password login**,
  via a new `UpdateLastLogin(ctx, userID) (*models.User, error)` method
  on `UserRepository`/`PostgresUserRepository` (new `UpdateUserLastLogin`
  sqlc query, mirrors `UpdateUserLoginProfile`'s shape but only touches
  `last_login_at`/`updated_at`). The column exists specifically to track
  this (`models/user.go:18`, currently populated only by Google's
  `refreshLoginProfile`) — leaving it permanently null for every
  password-authenticated user would make it silently mean "last Google
  login," a gap its own name doesn't suggest and nothing else is scoped
  to catch.
- **Login reuses `GoogleCallback`'s exact token-issuing tail**:
  `DeleteStaleFamiliesForUser` → `auth.GenerateRefreshToken` →
  `CreateFamily` → `setRefreshCookie` → `auth.GenerateAccessToken`.
  Confirmed safe to call unconditionally on every login —
  `DeleteStaleRefreshTokenFamiliesForUser`'s SQL
  (`internal/sqlc/queries/refresh_tokens.sql`) only deletes
  revoked-or-expired rows, never a user's other live-device sessions.
- **Status codes**: `invalid_credentials` → `401`,
  `google_account_no_password` → `401` (both are "no valid credential
  presented"), `email_not_confirmed` → `403` (the password *did* match —
  this is authorization being withheld for account state, not an
  authentication failure, so it gets its own status rather than
  overloading `401` for two different meanings).

## Flagged for `CFE-002`'s own grill (not resolved here)

`crockpot-react`'s spec (`master-spec.md:79`) plans a generic "401 →
refresh → retry" interceptor on its API client. `/auth/login` returning
`401` for a wrong password must not be routed through that interceptor —
there's no session yet to refresh, so it would silently retry-and-mask
the real "wrong password" error instead of surfacing it. `CFE-002` needs
to exclude `/auth/login` (and `/auth/register`, `/auth/confirm`) from
whatever triggers the refresh-retry behavior.

## Acceptance criteria

- [ ] `internal/sqlc/queries/users.sql`: `UpdateUserLastLogin` query
      added (`UPDATE users SET last_login_at = CURRENT_TIMESTAMP,
      updated_at = CURRENT_TIMESTAMP WHERE id = $1 RETURNING *`).
      `go tool sqlc generate` run after.
- [ ] `internal/repository/user.go`: `UpdateLastLogin(ctx, userID string)
      (*models.User, error)` added to `PostgresUserRepository`.
- [ ] `internal/handler/auth_handler.go`: `UserRepository` interface
      gains `UpdateLastLogin`; `mockery` regenerated
      (`go tool mockery`) so `mocks.MockUserRepository` picks it up.
- [ ] `internal/handler/auth_handler.go`: `loginRequest` struct
      (`Email`/`Password`, `binding:"required,email"`/`"required"`,
      matching `registerRequest`'s shape) + `Login` handler implementing
      the check order and error mapping from Decisions above.
- [ ] `main.go`: `POST /auth/login` wired into the existing `authTight`
      group (10 req/min/IP tier, same group as `register`/`confirm`/
      `resend-confirmation`) — no new rate-limit tier needed.
- [ ] `requests/auth.http`: new `POST /auth/login` section — a ✅ success
      case (against a pre-confirmed test account, capturing
      `@authToken = {{login.response.body.accessToken}}` per the
      existing `.http`-suite note in `crockpot-go/CLAUDE.md`), plus 🔒
      cases for wrong password, unknown email, unconfirmed account, and
      a Google-only account attempting password login.
- [ ] Handler tests (`internal/handler/auth_handler_test.go`,
      table-driven, `mocks.MockUserRepository` +
      `mocks.MockRefreshTokenRepository`): success (issues both tokens,
      calls `UpdateLastLogin`), wrong password, unknown email
      (`invalid_credentials`, same code as wrong password), unconfirmed
      account (`403 email_not_confirmed`, only reached after password
      matches), Google-only account (`401
      google_account_no_password`), malformed body (`400
      invalid_request`).
- [ ] Repository test (`internal/repository/user_test.go`, real Neon dev
      DB): `UpdateLastLogin` round-trip — `last_login_at` actually
      advances.
- [ ] A real login round-trip via `requests/auth.http` against the real
      Neon dev DB: correct password succeeds and returns a usable
      `accessToken`; wrong password, unconfirmed account, and a
      Google-only account each hit their documented error/status.
- [ ] `go test ./...` passes; `golangci-lint run --max-same-issues=0
      --max-issues-per-linter=0 ./...` clean; `gofmt` clean; `go mod
      tidy -diff` clean (ticket adds no new dependency, so this should
      be a no-op).

## Roadmap

1. **Schema/query layer** — add `UpdateUserLastLogin` to
   `internal/sqlc/queries/users.sql` (model it on
   `MarkUserEmailConfirmed`'s shape just above it, but only touch
   `last_login_at`/`updated_at`). Run `go tool sqlc generate`. No
   migration needed — `last_login_at` already exists on `users`
   (`db/migrations/000001_init.up.sql:13`).
2. **Repository** — add `UpdateLastLogin` to `PostgresUserRepository` in
   `internal/repository/user.go`, following `MarkEmailConfirmed`'s
   exact shape (lines 96-106: `uuidParam` → generated query call →
   `toModelUser`).
3. **Interface + mocks** — add `UpdateLastLogin` to the
   `UserRepository` interface in `internal/handler/auth_handler.go`
   (lines 30-35). Run `go tool mockery` to regenerate
   `internal/handler/mocks/mock_UserRepository.go`.
4. **Handler** — add `loginRequest` and `Login` to
   `internal/handler/auth_handler.go`, referencing:
   - `Register` (lines 216-280) for the request-binding /
     `invalid_request` pattern and the overall handler shape.
   - `bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash),
     []byte(req.Password))` for the password check — note
     `user.PasswordHash` is `*string` (nil for Google accounts), check
     nil *before* calling bcrypt, not as a bcrypt error branch.
   - `GoogleCallback`'s tail (lines 185-204) for the token-issuing
     sequence — copy the family-creation/cookie-setting calls verbatim,
     swap the final response for `c.JSON(http.StatusOK,
     gin.H{"accessToken": accessToken})` instead of a redirect.
   - Check order per Decisions: `FindByEmail` (not-found →
     `401 invalid_credentials`) → nil-`PasswordHash` check (→
     `401 google_account_no_password`) → `bcrypt.CompareHashAndPassword`
     mismatch (→ `401 invalid_credentials`) → `EmailVerifiedAt` nil
     check (→ `403 email_not_confirmed`) → `UpdateLastLogin` → issue
     tokens → `200`.
5. **Route wiring** — add `authTight.POST("/login", authHandler.Login)`
   in `main.go` alongside the existing three (around line 92-94).
6. **`.http` suite** — extend `requests/auth.http` per the AC above.
   Needs one pre-confirmed test account to log into — either reuse a
   confirmed account from a prior manual `register`/`confirm` run in
   the same file, or note the dependency at the top of the new section.
7. **Manual verification** — run the new `requests/auth.http` section
   against `crockpot-go` pointed at the real Neon dev DB: confirm each
   AC bullet's real trigger (success, wrong password, unknown email,
   unconfirmed, Google-only) actually returns the documented
   status/code, not just what the unit tests assert with mocks.
8. **Tests** — write the handler and repository tests from the AC list.
   Since this is hand-written mode, tests are written alongside/after
   the real implementation rather than test-first — no red-stage
   stubbing step required here (that requirement is specific to
   AI-driven mode's cadence).
9. **Lint/format/tidy** — `gofmt`, `golangci-lint run
   --max-same-issues=0 --max-issues-per-linter=0 ./...`, `go mod tidy
   -diff`.
10. **Open a PR** to trigger CodeRabbit (per `crockpot-go/CLAUDE.md`'s
    per-ticket review convention) — pull and address real findings,
    then close out.

## Non-goals

- No forgot/reset password (`CROC-007`) — that's its own ticket, and its
  handoff doc decides independently whether it reuses this ticket's
  error-taxonomy patterns (per `CROC-005`'s own non-goals note: reset is
  a different UX context, not assumed to mirror another ticket's shape).
- No `/auth/refresh` or `/auth/logout` (`CROC-008`) — this ticket mints a
  session but doesn't touch renewal/revocation beyond what
  `GoogleCallback` already established.
- No `GET /me` (`CROC-009`) — login intentionally returns no user object;
  the frontend's "who am I" resolution is entirely `/me`'s job.
- No account-level lockout mechanism (see Decisions) — IP rate-limiting
  only, for v1.
- No resolution of `CFE-002`'s interceptor-exclusion question — flagged
  above for that ticket's own grill.

## Verification modes

- **Logic with assertable behaviour**: `internal/handler/auth_handler_test.go`.
  `go test ./internal/handler/...`.
- **Service/API boundary**: `internal/repository/user_test.go`'s
  `UpdateLastLogin` case, against the real Neon dev `DATABASE_URL` —
  `DATABASE_URL=$DATABASE_URL go test ./internal/repository/...`.
- **Anything interactive**: real login round-trip via
  `requests/auth.http` against the running API + real Neon dev DB —
  the founder runs this by hand (per hand-written mode) as part of
  implementation, not just at close-out.
- **Limits/config**: no new numeric limits introduced this ticket (reuses
  the existing 10 req/min/IP tier and existing token TTLs) — nothing new
  to trigger beyond what `CROC-005`/`CROC-003` already exercised.
- **Lint**: `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff`.
- **Code review**: `code-review` available on request (hand-written
  mode) — ask before running rather than auto-launching. A PR should
  still be opened to get CodeRabbit's pass regardless, per
  `crockpot-go/CLAUDE.md`.

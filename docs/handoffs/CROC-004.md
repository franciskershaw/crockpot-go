# CROC-004 — Google OAuth login flow

Grilled 2026-08-16, not yet implemented.

**Implementation mode: AI-driven / spec-driven.** Same mode as CROC-002.
OAuth/`go-oidc` and `sqlc` are new to this stack, but the priority here is
landing a working session quickly to unblock the frontend milestone below
(contrast CROC-003, hand-written specifically for pure-logic familiarity).

## Context

Epic 2's first ticket, and the first ticket to touch `internal/auth`
(beyond CROC-003's JWT helpers), `internal/repository`, `internal/handler`,
and `internal/sqlc/queries` for real — all four are currently empty. Also
the first ticket with a real `sqlc` query, so it owns the "`sqlc generate`
exits 0" proof CROC-002 deferred as a standing rule rather than pinning to
a ticket number.

**Planned implementation order deviates from ticket numbering**:
CROC-004 → CROC-008 (Refresh + logout) → crockpot-react's initial auth
wiring (CFE-002) → CROC-005/006/007 (password auth). Google login +
refresh + logout is a complete, testable session on its own and doesn't
need password auth to prove out; the goal is a real frontend client to
test against as soon as a session actually works end-to-end, not to
build password auth first just because it's numbered earlier.

## Decisions and why

Claims checked against real source before any of this was decided:

- **OAuth verification: OIDC `id_token` via `coreos/go-oidc`, not Google's
  userinfo REST endpoint.** Checked both `packing-list-go/internal/auth/google.go`
  (the real, running code: OIDC discovery, `provider.Verifier`, local
  ID-token signature verification) and `crockpot-go/config/config.go`'s
  already-scaffolded `GoogleOAuth2Config` (copied verbatim from
  `packing-list-go/config/config.go` at CROC-001). `grep -rn
  "GoogleOAuth2Config"` across both repos turns up nothing outside each
  project's own `config.go` — it's dead code in the pattern being copied,
  and it lacks the `openid` scope Google requires before it will even
  issue an `id_token`. ID-token verification is the actual precedent and
  the standard mechanism for *authenticating* a sign-in (a signed
  assertion, verified offline against cached JWKS); the userinfo
  endpoint proves only that a caller holds a valid access token
  (authorization), and costs a live HTTP round-trip per login that OIDC
  verification avoids.
- **`GoogleOAuth2Config` deleted from `config.Config`**, along with
  `buildGoogleOAuth2Config` and `TestLoad_BuildsGoogleOAuth2Config`.
  Provably dead in the exact pattern being copied — no reason to carry it
  forward now that it's been caught. `GoogleClientID`/`GoogleClientSecret`/
  `GoogleRedirectURL` stay; `internal/auth.NewGoogleOAuthManager` builds
  its own `oauth2.Config` via OIDC discovery, same shape as
  `packing-list-go/internal/auth/google.go`.
- **Email collision with a password account is handled explicitly, not
  deferred.** `GetOrCreateUser` looks up by `google_id` first; on miss it
  inserts a new user. If that insert's email collides with an existing
  password-only row, the `users.email` UNIQUE constraint fires — a
  direct, foreseeable consequence of CROC-002's "mutually exclusive, no
  linking" decision, not a hypothetical to wave off until CROC-005/006
  exist. The repository detects the Postgres unique-violation
  (`pgconn.PgError`, code `23505`) on `email` and returns a typed
  `ErrEmailRegisteredWithPassword`, which the handler turns into a
  distinguishable outcome (next decision) instead of a raw 500.
- **`GoogleCallback` reports every failure by redirecting to the frontend
  with `?error=<code>`, not `c.JSON`.** Deliberate divergence from
  `packing-list-go`'s precedent (which returns JSON error bodies from
  this same endpoint) — this endpoint is a top-level browser navigation
  target, Google redirects the user's actual browser here, so a JSON
  body is a dead end for a real user, not just an API-contract detail.
  Covers: invalid/mismatched state, code-exchange failure, ID-token
  verification failure, `ErrEmailRegisteredWithPassword`, and any other
  repository error. Success path unchanged: redirect to
  `${FRONTEND_URL}/auth/callback` with the refresh cookie already set,
  no query param.
- **CROC-004 issues and persists only the refresh token; it does not mint
  an access token.** Matches `packing-list-go/internal/handler/auth_handler.go`'s
  actual `GoogleCallback` (its own comment: "No access token or user data
  in the redirect — the frontend mints its own access token via `POST
  /auth/refresh`"), and matches `crockpot-react`'s master-spec commitment
  (access token in memory, minted via `POST /auth/refresh` on app load).
  Consequence accepted explicitly: `POST /auth/refresh` doesn't exist
  until CROC-008, so CROC-004 alone can't be verified by obtaining a real,
  usable access token — same "first real consumer proves it" pattern
  CROC-003 used for `AuthMiddleware`; that proof lands with CROC-008.
  `RefreshTokenRepository` is one interface split across two tickets:
  CROC-004 defines and implements only `CreateFamily`/
  `DeleteStaleFamiliesForUser` (what `GoogleCallback` needs to persist a
  session); CROC-008 extends the same interface and `Postgres*` struct
  with `RotateFamily`/`FindFamilyByID`/`RevokeFamily`.
- **`users.last_login_at TIMESTAMPTZ` added as a schema addendum to
  CROC-002's migration.** `packing-list-go`'s `GetOrCreateUser` updates
  `last_login_at` on every repeat login; crockpot's schema has no
  equivalent (only `created_at`/`updated_at`). Free to capture in the
  same UPDATE the found-path already has to run — same "free information
  for free" reasoning CROC-002 used for `email_verified_at` as a
  timestamp instead of a boolean. Not a step toward the admin panel
  master-spec defers (no route or UI consumes it here) — just data
  captured now for whoever needs it later. `db/migrations/000001_init.up.sql`/
  `.down.sql` are amended **in place** (matching CROC-002's own precedent
  for pre-launch schema — nothing has shipped to production yet),
  verified with a real `down`/`up` cycle against the Neon dev DB, not a
  new `000002` migration.
- **`GetOrCreateUser`'s found-path also refreshes `name`/`image`** from
  the current Google claims, in the same UPDATE that sets
  `last_login_at` — no added query cost, keeps the stored profile from
  silently diverging from what Google actually has if the user changes
  their name or avatar there.

## Acceptance criteria

- [ ] `go.mod` gains `github.com/coreos/go-oidc/v3 v3.18.0` (matching
      `packing-list-go`'s pin); `golang.org/x/oauth2` and
      `github.com/google/uuid` are already present.
- [ ] `db/migrations/000001_init.up.sql`/`.down.sql` amended in place:
      `users.last_login_at TIMESTAMPTZ` added. `migrate up`/`down`/`up`
      all exit clean against the real Neon dev DB.
- [ ] `config/config.go`: `GoogleOAuth2Config` field,
      `buildGoogleOAuth2Config`, and `TestLoad_BuildsGoogleOAuth2Config`
      removed. `GoogleClientID`/`GoogleClientSecret`/`GoogleRedirectURL`
      stay as-is.
- [ ] `internal/auth/google.go`: `GoogleOAuthManager` —
      `NewGoogleOAuthManager(clientID, clientSecret, redirectURL,
      stateSecret string)` (OIDC discovery against
      `https://accounts.google.com`, `openid`/`email`/`profile` scopes),
      `GenerateState`/`ValidateState` (signed short-lived JWT, matching
      `packing-list-go`'s `oauthStateTTL` pattern), `GetAuthURL`,
      `ExchangeCodeForToken`, `VerifyIDToken` returning
      `IDTokenClaims{Email, GoogleID, DisplayName, AvatarURL}`.
      Structurally identical to `packing-list-go/internal/auth/google.go`.
- [ ] `internal/sqlc/queries/users.sql`: `GetUserByGoogleID`, `CreateUser`,
      `UpdateUserLoginProfile` (sets `last_login_at`, `name`, `image`) —
      real queries against `users`. `sqlc generate` exits 0 (this ticket
      owns that proof per CROC-002's standing rule).
- [ ] `internal/repository/user.go`:
      `PostgresUserRepository.GetOrCreateUser(ctx, email, googleID,
      displayName, avatarURL string) (*models.User, error)` — found path
      updates `last_login_at`/`name`/`image` and returns the refreshed
      row; miss path inserts and returns the new row; email-collision
      path returns `ErrEmailRegisteredWithPassword` (checked via
      `pgconn.PgError.Code == "23505"` on the `email` constraint, not the
      `google_id`/`password_hash` CHECK).
- [ ] `internal/sqlc/queries/refresh_tokens.sql` +
      `internal/repository/refresh_token.go`:
      `PostgresRefreshTokenRepository` implementing only
      `CreateFamily(ctx, id, userID, tokenHash string, expiresAt
      time.Time) (*models.RefreshTokenFamily, error)` and
      `DeleteStaleFamiliesForUser(ctx, userID string) error` — signatures
      matching `packing-list-go/internal/repository/refresh_token.go`.
      `RotateFamily`/`FindFamilyByID`/`RevokeFamily` are CROC-008's
      addition to this same interface/struct, not built here.
- [ ] `internal/handler/auth_handler.go`: `AuthHandler{userRepo,
      oauthManager, refreshTokenRepo, cfg}`, `LoginWithGoogle`
      (generate+cookie state, redirect to `GetAuthURL`), `GoogleCallback`
      (validate state, exchange code, verify ID token, `GetOrCreateUser`,
      generate+persist refresh token family, set httponly refresh
      cookie, redirect to `${FRONTEND_URL}/auth/callback`). All failure
      paths redirect to `${FRONTEND_URL}/auth/callback?error=<code>`,
      never `c.JSON`, per the decision above.
- [ ] Refresh cookie attributes match `packing-list-go`'s
      `setRefreshCookie`: `SameSite=None` in production, `Lax` in
      development; httponly; `Secure` in production.
- [ ] `main.go`: `GET /auth/google/login`, `GET /auth/google/callback`
      wired to the new handler.
- [ ] `internal/auth/google_test.go`: `TestGenerateState`,
      `TestValidateState_ValidToken`, `TestValidateState_InvalidSignature`,
      `TestValidateState_Expired`, `TestGetAuthURL` — ported from
      `packing-list-go/internal/auth/google_test.go`'s fake-config
      pattern (no live OIDC discovery call in tests).
      `ExchangeCodeForToken`/`VerifyIDToken` are deliberately not
      unit-tested here either, matching that file's own precedent —
      proven only by the real browser flow (see Verification modes).
- [ ] `internal/handler/auth_handler_test.go`: table-driven
      `TestGoogleCallback` over `testify/mock`-backed
      `UserRepository`/`OAuthManager`/`RefreshTokenRepository` — success
      path, invalid/mismatched state, exchange failure, verify failure,
      `ErrEmailRegisteredWithPassword`, generic repository error —
      asserting the right redirect target and `?error=` code (or
      absence) for each.
- [ ] `internal/repository/user_test.go`,
      `internal/repository/refresh_token_test.go`: real integration
      tests against `DATABASE_URL` (Neon dev, never Docker/local
      Postgres) — create-then-find, found-path refreshes
      `last_login_at`/`name`/`image`, email-collision returns
      `ErrEmailRegisteredWithPassword`, `CreateFamily`/
      `DeleteStaleFamiliesForUser` round-trip.
- [ ] A real manual Google OAuth flow, browser-driven, against the real
      Google consent screen and the real Neon dev DB: hitting
      `/auth/google/login` redirects to Google, completing consent
      redirects back through `/auth/google/callback` to
      `${FRONTEND_URL}/auth/callback`, a `users` row and a
      `refresh_tokens` row exist afterward with correct data, and the
      refresh cookie is present with the right attributes. Re-running the
      same flow with the same Google account hits the found path
      (`last_login_at` advances, row count unchanged).
- [ ] `go test ./...` passes; `golangci-lint run --max-same-issues=0
      --max-issues-per-linter=0 ./...` clean.

## Non-goals

- No access-token minting in this ticket — `POST /auth/refresh` is
  CROC-008.
- No `RotateFamily`/`FindFamilyByID`/`RevokeFamily` — CROC-008 extends
  `RefreshTokenRepository` with these.
- No password auth (CROC-005/006/007) — planned to land after CROC-008
  and the frontend's initial auth wiring (CFE-002), not before, per the
  reordering above.
- No admin UI or route consuming `last_login_at` — captured now, consumed
  whenever a future ticket wants it.
- No account linking — a Google sign-in against an email already
  registered via password fails with `ErrEmailRegisteredWithPassword`, it
  does not merge accounts (CROC-002's standing decision).

## Verification modes

- **Logic with assertable behaviour**: `internal/auth/google_test.go`
  (state generate/validate/expiry/tamper, `GetAuthURL`) and
  `internal/handler/auth_handler_test.go` (`GoogleCallback`'s branches
  over mocked repositories) — failing test first, confirm red, then
  implement. `go test ./internal/auth/... ./internal/handler/...`.
- **Service/API boundary**: `internal/repository/user_test.go`/
  `refresh_token_test.go` — real queries against the real Neon dev
  `DATABASE_URL`, not mocked, per this project's repository-test
  convention.
- **Anything interactive**: the real Google OAuth consent flow, hands-on,
  through an actual browser — `ExchangeCodeForToken`/`VerifyIDToken` are
  only proven this way (matching `packing-list-go/internal/auth/google_test.go`'s
  own precedent of leaving these two untested at the unit level). This is
  also the only real proof that the first `sqlc generate` works and that
  the refresh cookie actually gets set with the right attributes.
- **Limits/config**: N/A beyond what's already fixed (10-minute OAuth
  state TTL, matching `packing-list-go`) — not new numbers being tuned
  here.
- **Lint**: `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...`.
- **Code review**: `/code-review` on the full diff before close-out, same
  gate as every prior ticket.

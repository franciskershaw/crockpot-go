# CROC-007 — Forgot/reset password

Grilled 2026-08-16. Completed 2026-08-17.

**Implementation mode: AI-driven.** Claude implements one piece at a time
(failing test/stub → confirm red → stop for go-ahead → implement →
confirm green → stop before the next piece), per
`crockpot-go/CLAUDE.md`'s AI-driven cadence.

## Context

`POST /auth/forgot-password` + `POST /auth/reset-password` are the fourth
of five Epic 2 tickets (`CROC-005` → `006` → **007** → `008` → `009`),
sequenced per the correction at `CROC-005`'s grill.

**No functional or architectural precedent exists for this ticket**,
same finding as `CROC-005`: checked `crockpot/prisma/schema.prisma` and
`crockpot/src` (the old Next.js reference) for any forgot/reset-password
model or route — none exists, matching `bcryptjs` being an unused
dependency there. `packing-list-go` is Google-OAuth-only, no password
flow at all. This design is first-principles, same posture as CROC-005.

`master-spec.md:230-232` already flagged this ticket's token shape as
genuinely open ("shape TBD at CROC-007's own grill — not assumed to be
the same OTP pattern"), and `CROC-005`'s own non-goals section
(`docs/handoffs/CROC-005.md:259-262`) flagged the same thing from the
other side. Nothing here overrides an existing decision; everything
below is a first answer to a question that was deliberately left open.

## Decisions and why

- **Reset token is an opaque, high-entropy link-token, not the
  email-confirmation OTP pattern.** New `auth.GenerateResetToken() (string,
  error)` — 32 random bytes (`crypto/rand`), hex-encoded — hashed via the
  existing `auth.HashToken` (`internal/auth/token.go:19-22`, already
  shared by refresh tokens and confirmation codes). Delivered as a link
  (`${FRONTEND_URL}/reset-password?token=...`), not a typed code.
  Reasoning:
  - The schema is already consistent with this: `password_reset_tokens`
    (`db/migrations/000001_init.up.sql:51-58`) has no `attempts` column,
    unlike `email_verification_tokens`, which got one added at `CROC-005`
    specifically because a 6-digit OTP's 1,000,000-value guess-space
    needs a lockout. A 32-byte token has no realistic guess-space, so it
    needs none. Flagged as *consistent with*, not proof of, this
    decision — the schema predates this ticket's grill.
  - The founder's reason for rejecting a link for email confirmation
    (reads as spam/phishing right after signup, `master-spec.md:120-123`)
    doesn't transfer to password reset — a "click here to reset your
    password" link is one of the most standardized, expected email
    patterns that exists, the opposite signal from an unsolicited
    account-confirmation link.
  - UX: forgot-password's natural flow is submit email → check inbox →
    land on "set new password" already-identified by the link, one tap.
    An OTP would add a copy-the-code step for no security gain given the
    entropy gap above.
- **`RevokeFamily`/`RotateFamily`/`FindFamilyByID` split at
  `master-spec.md:166-167` is corrected**: `CROC-004` builds
  `CreateFamily`/`DeleteStaleFamiliesForUser`; **this ticket** adds
  `RevokeAllFamiliesForUser(ctx, userID string) error` (bulk, revokes
  every live family for a user — "log out everywhere"); `CROC-008` adds
  `RotateFamily`/`FindFamilyByID`/`RevokeFamily` (all single-family,
  by-ID, for the rotate-on-use + reuse-detection mechanism). These are
  complementary, not overlapping — bulk logout-on-reset vs. per-family
  reuse revocation are different operations. Corrected here because
  `ResetPassword` needs to revoke every other live session: checked
  `internal/repository/refresh_token.go` and confirmed
  `DeleteStaleFamiliesForUser` only ever deletes rows already
  revoked-or-expired (`refresh_tokens.sql`'s `WHERE revoked_at IS NOT
  NULL OR expires_at < CURRENT_TIMESTAMP`) — there is currently no way to
  kill a *live* session at all. A password reset whose whole premise is
  usually "I no longer trust this credential" (leaked/reused password)
  doesn't close that hole if a live session survives it unchanged.
- **`POST /auth/forgot-password` discloses unknown-email and
  Google-only-account cases explicitly**, matching this project's
  existing, repeated posture rather than the generic "if that email has
  an account..." response security guides usually default to for this
  endpoint. Checked current code before assuming precedent applies:
  `Register` already discloses existing accounts via distinct `409`s
  (`auth_handler.go:248-253`); `ResendConfirmation` already discloses
  "no account at all" via `400 email_not_found`
  (`auth_handler.go:409-418`) at the same friction level (unauthenticated,
  email-only body) forgot-password sits at; `Login` already discloses
  Google-only accounts via `401 google_account_no_password`
  (`auth_handler.go:467-469`). Three prior tickets making the same call
  is a house style (the project's own "not a bank" posture,
  `master-spec.md`), not something to quietly reverse on a fourth. Since
  `ResendConfirmation` already provides a full account-existence oracle
  at the same friction level, a generic response here wouldn't close any
  real channel — it would just be inconsistent with one that already
  exists.
  - Unknown email → `400 email_not_found` (reuses `ResendConfirmation`'s
    exact code).
  - Google-only account → `401 google_account_no_password` (reuses
    `Login`'s exact code).
  - Otherwise → issue + email the token, `200` generic success message.
- **Forgot/reset works for unconfirmed accounts, and a successful reset
  also marks the email confirmed.** An unconfirmed account already has a
  real `password_hash` (set at `Register`) — "forgot my password before
  confirming" is a normal sequence, not a broken state. Clicking a reset
  link proves inbox control exactly as much as typing the OTP does, so
  withholding confirmation after a successful reset would make the user
  prove the same thing twice for no security gain. Implementation: call
  the existing `MarkEmailConfirmed` (`user.go:96-106`) alongside the new
  `UpdatePassword` call — both idempotent-safe if the account was already
  confirmed.
- **`ResetPassword` auto-logs-in the requester** — same token-issuing
  tail as `Login`/`GoogleCallback` (`issueRefreshSession` +
  `auth.GenerateAccessToken`), response shape `{"accessToken": "..."}`
  matching `Login`'s. Different case from `Register`'s "issues no
  tokens" call (`CROC-005`) — that withheld a session specifically
  because the email wasn't confirmed yet, which no longer applies once
  reset itself confirms it (previous decision). By the time this handler
  returns, the caller has proven both inbox control (the token) and
  the new password — a second, separate login proves nothing further.
  **Ordering, per the `CROC-006` close-out lesson** ("the step that
  grants access goes last, protect against a live session existing when
  the response says it failed"): revoke-all-other-families and the
  password/confirmation update happen first; `issueRefreshSession` (which
  sets the live cookie) runs last, right before the response. This also
  composes correctly with the revoke: `issueRefreshSession`'s existing
  `DeleteStaleFamiliesForUser` call will find the just-revoked rows
  already `revoked_at`-stamped and clean them up as it always does — no
  conflict, no new logic needed there.
- **Reset token TTL: 1 hour.** Confirms the `master-spec.md:231`
  placeholder ("reset ~1h — confirmed per-ticket, not locked in at
  kickoff"). Long enough that switching devices between request and
  click doesn't expire it, short enough to bound exposure if the email
  itself is compromised later.
- **Same rate-limit shape as `CROC-005`'s confirmation-code issuance**:
  `authTight` tier (10 req/min/IP) on both endpoints, plus a 60s
  per-email cooldown on `/auth/forgot-password` inside a new
  `issuePasswordResetToken` helper mirroring `issueConfirmationCode`'s
  shape (`auth_handler.go:296-328`) — same reasoning, no proof of email
  ownership at that endpoint, so per-IP alone doesn't stop someone
  spamming a stranger's inbox from spread-out requests. Same
  "clear-before-insert" op (`DeleteActiveForUser`) before issuing a fresh
  token, same pattern `master-spec.md:230-231` already locks in project-
  wide.
- **`UpdatePassword` is a new, narrowly-scoped `UserRepository` method**,
  not a revival of `UpdatePasswordAndClearConfirmation` (deleted at
  `CROC-005` — see its handoff's correction). That method was deleted
  because it was reachable from email alone (an abandoned-signup retry);
  this ticket's own non-goals note at `CROC-005` already anticipated the
  fix: "`CROC-007`'s reset flow will be gated on a proven reset token,
  not email-alone, so it doesn't revive this method's use case." Confirmed
  true here — `ResetPassword` only ever reaches `UpdatePassword` after
  validating a hashed, unexpired, unused, token-hash-matched row.

## Acceptance criteria

- [ ] `internal/auth/token.go`: `GenerateResetToken() (string, error)` —
      32 random bytes via `crypto/rand`, hex-encoded.
- [ ] `internal/sqlc/queries/password_reset_tokens.sql` +
      `internal/repository/password_reset_token.go`:
      `PostgresPasswordResetTokenRepository` — `Create`,
      `FindActiveByUserID` (issuance/cooldown check), `FindActiveByTokenHash`
      (consumption), `MarkUsed`, `DeleteActiveForUser`.
- [ ] `internal/models/errors.go`: `ErrNoActivePasswordResetToken`
      (mirrors `ErrNoActiveEmailVerificationToken`).
- [ ] `internal/sqlc/queries/refresh_tokens.sql`:
      `RevokeAllRefreshTokenFamiliesForUser :exec` — `UPDATE
      refresh_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE user_id =
      $1 AND revoked_at IS NULL`. `internal/repository/refresh_token.go`:
      `RevokeAllFamiliesForUser(ctx, userID string) error` added to
      `PostgresRefreshTokenRepository`.
- [ ] `internal/sqlc/queries/users.sql`: `UpdateUserPassword` query
      (models on `UpdateUserLastLogin`'s shape, sets `password_hash` +
      `updated_at`). `internal/repository/user.go`: `UpdatePassword(ctx,
      userID, passwordHash string) (*models.User, error)` added to
      `PostgresUserRepository`.
- [ ] `internal/handler/auth_handler.go`: `RefreshTokenRepository` gains
      `RevokeAllFamiliesForUser`; `UserRepository` gains `UpdatePassword`;
      new `PasswordResetTokenRepository` interface (owned by `handler`,
      matching this project's consumer-defined convention) injected into
      `AuthHandler`; `EmailSender` gains `SendPasswordResetLink(ctx,
      toEmail, resetURL string) error`. `mockery` regenerated for all
      four.
- [ ] `internal/email/templates/reset.html` + `reset.txt` +
      `internal/email/resend.go`: `SendPasswordResetLink` implementation,
      mirrors `SendConfirmationCode`'s render/send shape
      (`resend.go:54-94`).
- [ ] `internal/handler/auth_handler.go`: `forgotPasswordRequest{Email}`,
      `ForgotPassword` handler — check order: `FindByEmail` (not found →
      `400 email_not_found`) → nil-`PasswordHash` check (→ `401
      google_account_no_password`) → `issuePasswordResetToken` (new
      helper, mirrors `issueConfirmationCode`: cooldown check →
      clear-before-insert → generate → persist → email; same
      `errResendCooldown` → `429 resend_too_soon` mapping) → `200`
      generic success message.
- [ ] `internal/handler/auth_handler.go`: `resetPasswordRequest{Token,
      NewPassword}`, `ResetPassword` handler — check order:
      `FindActiveByTokenHash(auth.HashToken(req.Token))` (not found → `400
      token_invalid`) → expiry check (→ `400 token_expired`) → password
      length checks (reuse `minPasswordLength`/`maxPasswordBytes`, same
      `password_too_short`/`password_too_long` codes as `Register`) →
      `UpdatePassword` → `MarkEmailConfirmed` → `MarkUsed` on the token →
      `RevokeAllFamiliesForUser` → `issueRefreshSession` (last, per the
      ordering note above) → `200 {"accessToken": ...}`.
- [ ] `main.go`: `POST /auth/forgot-password`, `POST /auth/reset-password`
      wired into the existing `authTight` group.
- [ ] `requests/auth.http`: new sections for both endpoints — ✅ success
      (forgot-password against a confirmed test account, capturing the
      token from the dev-DB row since there's no real-email step
      automatable here beyond what `CROC-005`'s manual round-trip already
      covers; reset-password consuming it), plus 🔒 cases for unknown
      email, Google-only account, invalid/expired/reused token, password
      too short/long, and the 60s cooldown.
- [ ] Handler tests (`internal/handler/auth_handler_test.go`,
      table-driven, `mocks`-backed repos): forgot-password success,
      unknown email, Google-only account, cooldown active;
      reset-password success (asserts `UpdatePassword`, `MarkEmailConfirmed`,
      `MarkUsed`, `RevokeAllFamiliesForUser`, and `issueRefreshSession`'s
      tail all get called, in that order), invalid token, expired token,
      already-used token, password too short/long, malformed body on
      both.
- [ ] Repository tests (real Neon dev DB): `password_reset_token_test.go`
      (create/find round-trips, `DeleteActiveForUser` clears before
      insert), `refresh_token_test.go` (`RevokeAllFamiliesForUser` round
      trip — asserts other live families get `revoked_at` set, doesn't
      touch already-revoked/expired ones), `user_test.go`
      (`UpdatePassword` round trip).
- [ ] A real forgot-password → real email received via Resend → real
      reset-password round trip, against the real Neon dev DB and real
      Resend API (not mocked) — same "email delivery can't be proven by a
      mock" rule `CROC-005` established.
- [ ] Real triggers via `requests/auth.http` for: an expired token (1h+
      old, or `expires_at` backdated in dev DB), a reused/already-used
      token, two forgot-password calls within 60s hitting the cooldown, a
      73-byte new-password hitting the explicit reject, and confirming a
      pre-existing other-device session (a second `refresh_tokens` row
      for the same user, live) actually gets `revoked_at` set after
      reset.
- [ ] `go test ./...` passes; `golangci-lint run --max-same-issues=0
      --max-issues-per-linter=0 ./...` clean; `gofmt` clean; `go mod tidy
      -diff` clean.
- [ ] `docs/specs/master-spec.md` updated: the `RefreshTokenRepository`
      interface-split note (currently `master-spec.md:166-167`) corrected
      per the Decisions section above; the CROC-007 backlog entry
      replaced with the actual decided shape (was written as "same
      clear-before-insert requirement as CROC-005" only, no token shape);
      the reset-token TTL placeholder in "Session & revocation lifecycle"
      confirmed at 1h.

## Non-goals

- No account-level lockout on `/auth/forgot-password` or
  `/auth/reset-password` beyond the existing `authTight` IP tier and the
  new per-email cooldown — matches `CROC-006`'s "no account-level
  lockout" call; a 32-byte token has no brute-forceable keyspace, so
  there's nothing a lockout would be defending against here that rate
  limiting doesn't already cover.
- No decision on the exact frontend route shape
  (`/reset-password?token=...` is this ticket's working assumption for
  building the email link) — `crockpot-react` hasn't built its forgot/
  reset forms yet (waits for all of Epic 2, per the standing sequencing
  note); flagged for that ticket's own grill to confirm the route name
  matches, same shape as `CROC-006`'s `CFE-002` flag.
- No change to `ConfirmEmail`/`ResendConfirmation`'s own OTP mechanism —
  this ticket's token-shape decision is scoped to password reset only,
  doesn't retroactively question CROC-005's choice.
- No transactional wrapping of the multi-step `ResetPassword` write
  sequence (`UpdatePassword` → `MarkEmailConfirmed` → `MarkUsed` →
  `RevokeAllFamiliesForUser`) — matches this codebase's existing
  granularity (`Register`'s user-creation + code-issuance aren't
  transactional either); a partial failure here is a `server_error` on an
  otherwise-successful state change, not a security gap, since every step
  after the first is idempotent-safe to retry.

## Verification modes

- **Logic with assertable behaviour**: `internal/handler/auth_handler_test.go`
  — failing test first, confirm red, then implement. `go test
  ./internal/handler/... ./internal/auth/...`.
- **Service/API boundary**: `internal/repository/password_reset_token_test.go`,
  `refresh_token_test.go`, `user_test.go` against the real Neon dev DB —
  `./scripts/test-repo.sh`; and a real Resend send for the reset-link
  email (see AC) — delivery can't be meaningfully proven by a mock.
- **Limits/config**: the 1h TTL, 60s cooldown, and the revoke-all-on-reset
  behavior are all new/first-real-exercise here — each gets a real
  trigger through `requests/auth.http`, not just a unit test with a
  mocked clock (see AC).
- **Lint**: `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff`.
- **Code review**: per-ticket review is CodeRabbit's job via PR, per
  `crockpot-go/CLAUDE.md` — open/update the PR once the diff is done,
  pull and address real findings, then close out.

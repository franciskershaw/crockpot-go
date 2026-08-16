# CROC-005 — Email/password registration + confirmation

Grilled 2026-08-16. Completed 2026-08-16.

**Implementation mode: AI-driven / spec-driven.** Mostly composes patterns
that already exist in this codebase (sha256 token hashing like refresh
tokens, repository/handler interface pattern, `ulule/limiter` ported from
`packing-list-go`) plus one genuinely new piece (Resend HTTP integration)
that doesn't warrant hand-written/hands-on-familiarity mode on its own.

## Context

**Sequencing correction**: the plan noted at `CROC-004`
(`docs/handoffs/CROC-004.md`) — CROC-004 → CROC-008 → `crockpot-react`'s
CFE-002 → CROC-005/006/007 — is superseded. Epic 2 now completes in full
ticket order (CROC-005 → 006 → 007 → 008 → 009) before any `crockpot-react`
work starts. The original reasoning (get a real frontend client working
against a session ASAP) was a misread on the founder's side of what "004 →
008" meant as a plan, not a deliberate choice to interleave; the founder's
actual bar for starting frontend work is the whole auth epic being done,
not just Google login + refresh/logout.

**No functional or architectural precedent exists for this ticket**,
despite `master-spec.md:122-123` claiming password hashing "match[es] the
old Next.js app's bcryptjs choice." Checked before designing anything
(global process rule: verify precedent claims against real source):

- `crockpot/prisma/schema.prisma`'s `User` model has no password field.
- `crockpot/src/auth.ts` — the real, running Auth.js config — is Google +
  a passwordless magic-link "Resend" provider. No Credentials provider
  ever existed.
- `bcryptjs` is a `crockpot/package.json` dependency, but `git log --all
  -S"bcrypt" -- src` returns zero commits — never imported or called.
- `packing-list-go` (the architectural reference) has no
  `bcrypt`/`password_hash` anywhere either — it's Google-OAuth-only.

Confirmed with the founder this ticket proceeds anyway (email/password is
a genuinely new feature for Crockpot, not a port) — every design decision
below is therefore first-principles, not "matches the reference."

**First ticket to need**: an `internal/email` package (no email-sending
code exists in this Go codebase yet), Resend API integration, and
rate-limiting middleware (`ulule/limiter` isn't in `go.mod` yet — no
ticket previously owned building it, and this is the first ticket that
actually needs it).

## Decisions and why

- **Email verification is a 6-digit numeric OTP code, not a clickable
  link.** Originally speced (and half-built in this grill session) as a
  signed JWT matching the refresh-token pattern (`auth/jwt.go`'s
  `GenerateRefreshToken`, hash stored via `sha256.Sum256` per
  `auth_handler.go:27`) delivered via a link (`GET /auth/confirm?token=`,
  as literally written in the original backlog text). Reversed after the
  founder flagged that a clickable confirmation link reads as spam/phishing
  in the wrong context — a typed code (the same pattern as Tesco's app,
  or any standard email-OTP flow) avoids that entirely. Consequence:
  **no JWT, no new JWT secret** — a JWT can't be hand-typed, so the code
  is just `crypto/rand`-generated digits, hashed via sha256 into
  `token_hash` the same way refresh tokens are, with no signing step.
  Removes complexity versus the link-based design, doesn't add it.
- **10-minute TTL**, down from the original 24h placeholder
  (`master-spec.md:202`, itself never actually confirmed — just a
  kickoff guess). A link can be opened later in the day (phone signup,
  checked on a laptop that evening) so a long TTL costs little; a typed
  code is a same-session action (open email, copy, come straight back),
  and its short TTL is also its primary defense against its own small
  guess-space (see next point) — not just a UX number.
- **Lockout after 5 wrong attempts** invalidates the code and requires a
  resend. A 6-digit code has only 1,000,000 possible values — trivial to
  brute-force without this, unlike the unguessable JWT the original design
  used. Combined with the 10-minute TTL and the auth-endpoint rate-limit
  tier below, this closes the gap the shorter/simpler code format opens.
  Requires a new `attempts INTEGER NOT NULL DEFAULT 0` column on
  `email_verification_tokens`, amended into `db/migrations/000001_init.up/
  .down.sql` **in place** (matching `CROC-004`'s precedent for pre-launch
  schema — nothing has shipped to production yet), not a new migration
  file.
- **`POST /auth/register` issues no tokens.** Returns `201` with a
  generic "check your email for a confirmation code" message. Matches
  `CROC-006`'s stated behaviour (rejects unverified logins with a
  distinct error) — register shouldn't hand out a working session that
  login itself refuses to grant.
- **Password: min 8 chars, max 72 bytes, the max rejected explicitly.**
  `golang.org/x/crypto/bcrypt` silently truncates input beyond 72 bytes —
  two different passwords sharing the same 72-byte prefix would hash
  identically if the API doesn't reject the excess itself. No reference
  precedent for these numbers (see Context) — first-principles minimums,
  revisit if real signups show the bar's wrong.
- **`POST /auth/confirm` takes `{email, code}` in the body, not a URL
  token.** The frontend already holds the just-registered email through
  the "check your email" screen; POST because a state-changing action
  with a guessable secret belongs in a body, not a URL (server access
  logs, browser history). Unknown email or no pending token returns the
  **same generic `code_invalid`** as a wrong code — deliberately not a
  distinct "no such registration" error, so this endpoint doesn't open a
  second enumeration channel next to register's own (see below).
- **`POST /auth/resend-confirmation` is a dedicated endpoint**, `{email}`
  only, clearing the prior unconsumed token and attempts count before
  issuing a fresh code (the "clear before insert" rule already noted in
  `master-spec.md:230-231`). A 10-minute code makes resend a common,
  expected action, not an edge case — re-submitting the full register
  form (password included) just to get a new code is bad UX. Gets its
  own **60-second per-email cooldown** (checked against the current
  token row's `created_at`, no new table) on top of the general per-IP
  auth rate-limit tier: unlike register/confirm, resend needs no proof of
  email ownership, so a per-IP-only limit doesn't stop someone spamming a
  stranger's inbox from spread-out requests/IPs.
- **Duplicate email on register, three cases, decided as a UX/security
  tradeoff (enumeration risk vs. dead-end UX), not defaulted either way**:
  - Confirmed password account exists → `409 email_already_registered`.
    Revealing this is a low-severity enumeration surface (this is a
    recipe app, not a bank — matches the "no compliance driver for this
    personal app" posture already established for role-staleness and
    no-soft-delete elsewhere in this spec), and not revealing it leaves
    the user with no path back to login/forgot-password.
  - Google account exists → `409 email_registered_with_google`, the
    mirror of `CROC-004`'s `ErrEmailRegisteredWithPassword`. Silence here
    is a worse UX dead-end than the password case (no other way for the
    user to guess what happened), and `CROC-004` already set the
    precedent of surfacing this exact collision explicitly.
  - Unconfirmed password account exists (abandoned signup) → **silently
    treated as a resend**: clear the old token/attempts, issue a new
    code, same `201` response as a fresh register. **Correction, caught
    by CodeRabbit's review of this ticket's diff**: the original version
    of this decision also overwrote `password_hash` here, reasoned as
    safe because "confirmation still only ever reaches the real inbox, so
    nobody can complete the account takeover this looks like." That
    reasoning was wrong — it only considered whether the *attacker* could
    complete confirmation, not what happens when the *legitimate owner*
    does. The real owner already has a valid code in their inbox from
    their own earlier registration; if an attacker re-registers the same
    unconfirmed email with a chosen password in between, the owner's own
    confirmation activates the attacker's password, not theirs. Fixed:
    case (c) never touches `password_hash` at all now — it's identical to
    calling `POST /auth/resend-confirmation`, nothing more.
    `UpdatePasswordAndClearConfirmation` (repository method, interface
    method, sqlc query, and its tests) is deleted as a result — no longer
    has any caller, and no future one: `CROC-007`'s reset flow will be
    gated on a proven reset token, not email-alone, so it doesn't revive
    this method's use case.
- **Rate-limiting middleware is built in this ticket**, not split into a
  separate foundational ticket, since nothing in the backlog previously
  owned it and this ticket is the first real consumer. Ports
  `packing-list-go/internal/middleware/rate_limit.go`'s `ulule/limiter`
  setup (real, proven precedent) — global 120 req/min/IP tier applied in
  `main.go` to all routes, a 10 req/min/IP auth-endpoint tier applied to
  `/auth/register`, `/auth/confirm`, `/auth/resend-confirmation`. The
  per-email resend cooldown above is this ticket's own new logic on top,
  not part of the port.
- **`requests/auth.http` starts in this ticket**, not `CROC-008` as
  `crockpot-go/CLAUDE.md` currently states. That carve-out's stated reason
  (no token available until `/auth/refresh` exists) doesn't block this
  ticket — `register`/`confirm`/`resend-confirmation` are all
  unauthenticated by definition. The CLAUDE.md line assumed the old
  sequencing (CROC-008 directly after CROC-004); fixed as part of this
  ticket's own scope, see below.
- **API error response shape formalized as a project-wide convention**,
  not just this ticket's own choice — flagged when the founder noticed
  `Register`'s `invalid_request` response was undifferentiated and asked
  whether it was deliberate. It wasn't, explicitly, until now: every
  JSON-responding endpoint returns `{"error": "snake_case_code"}` on
  failure, `{"message": "..."}` on non-resource success.
  `GoogleCallback`'s redirect-based `?error=code` shape stays the
  deliberate exception (browser navigation target, not a JSON consumer).
  Binding validation failures stay one generic `invalid_request`, no
  field-level detail — `crockpot-react`'s forms validate client-side
  first, so a real `invalid_request` is an edge case, not a normal
  user path worth building precision for. Recorded in full at
  `master-spec.md`'s "API error response shape" bullet.
- **The 60s resend cooldown lives inside `issueConfirmationCode` itself**,
  not `ResendConfirmation`'s handler body — found after the founder asked
  why `POST /auth/register`'s abandoned-signup retry path (case (c))
  wasn't cooldown-protected the same way `ResendConfirmation` is, given
  both have the identical effect of sending another code. Re-registering
  the same unconfirmed email repeatedly was a second, unprotected way to
  hit the exact email-bombing vector the cooldown exists for. Centralizing
  the check in the shared helper (a typed `errResendCooldown`, mapped to
  `429 resend_too_soon` at each call site) means every current and future
  caller gets the protection automatically, rather than trusting each one
  to remember to duplicate the check.

## Acceptance criteria

- [ ] `db/migrations/000001_init.up.sql`/`.down.sql` amended in place:
      `email_verification_tokens.attempts INTEGER NOT NULL DEFAULT 0`
      added. `migrate up`/`down`/`up` all exit clean against the real
      Neon dev DB.
- [ ] `config/config.go`: `RESEND_API_KEY`, `EMAIL_FROM` added, validated
      present at startup like the existing required vars.
- [ ] `internal/middleware/rate_limit.go`: ported from
      `packing-list-go/internal/middleware/rate_limit.go`. Global
      120/min/IP tier wired in `main.go`; a separate 10/min/IP tier
      constructor available for auth routes.
- [ ] `internal/email/resend.go`: `EmailSender` interface (owned by
      `internal/handler`, matching this project's consumer-defined
      interface convention) with `SendConfirmationCode(ctx, toEmail,
      code string) error`; `ResendClient` implementation.
- [ ] `internal/auth`: `GenerateConfirmationCode() (string, error)` —
      6-digit, `crypto/rand`; `HashToken(token string) string` — sha256,
      matching `auth_handler.go:27`'s existing inline pattern, extracted
      so both refresh tokens and this ticket's codes use the same helper.
- [ ] `internal/sqlc/queries/email_verification_tokens.sql` +
      `internal/repository/email_verification_token.go`:
      `PostgresEmailVerificationTokenRepository` — `Create`,
      `FindActiveByUserID`, `IncrementAttempts`, `MarkUsed`,
      `DeleteActiveForUser` (the "clear before insert" op).
- [ ] `internal/repository/user.go`: `CreateUnconfirmedUser`, `FindByEmail`,
      `MarkEmailConfirmed`. Duplicate-email detection via
      `pgconn.PgError.Code == "23505"` on `users.email`, distinguishing
      Google vs. password existing rows to pick the right typed error.
      Case (c) (abandoned unconfirmed signup) reuses `FindByEmail` —
      never a password-mutating method, see the correction above.
- [ ] `internal/handler/auth_handler.go`: `Register`, `ConfirmEmail`,
      `ResendConfirmation` — behaviour per the Decisions section above,
      including the 60s per-email resend cooldown and the 5-attempt
      lockout.
- [ ] `main.go`: `POST /auth/register`, `POST /auth/confirm`, `POST
      /auth/resend-confirmation` wired, auth-tier rate limit applied to
      all three.
- [ ] `requests/auth.http` created (new file — first ticket to need it,
      see Decisions above): manual requests for register, confirm,
      resend-confirmation, all token-free.
- [ ] `crockpot-go/CLAUDE.md`'s `.http` section updated: "First ticket
      this applies to: CROC-008" corrected to CROC-005; the reasoning
      updated to note register/confirm/resend need no token.
- [ ] `crockpot-go/docs/specs/master-spec.md`: Epic 2 header note
      replaced with the corrected full-epic-before-frontend sequencing;
      CROC-005's backlog entry updated to match the OTP design (was
      written as `GET /auth/confirm?token=`).
- [ ] Handler tests (`internal/handler/auth_handler_test.go`,
      table-driven, `testify/mock`-backed `UserRepository`/
      `EmailVerificationTokenRepository`/`EmailSender`): register
      success, register against each of the three duplicate-email cases,
      password too short/too long, confirm success, confirm wrong code,
      confirm expired code, confirm after 5 failed attempts, resend
      success, resend within cooldown.
- [ ] Repository tests (`internal/repository/user_test.go`,
      `email_verification_token_test.go`), real Neon dev DB: create/find
      round-trips, `attempts` increments, `DeleteActiveForUser` clears
      before a new insert (the unique partial index doesn't do this for
      you — same note `CROC-002` already flagged).
- [ ] A real register → real email received via Resend → real code typed
      into `POST /auth/confirm` round-trip, against the real Neon dev DB
      and the real Resend API (not mocked) — the only real proof the
      email actually sends and the copy reads correctly.
- [ ] Real triggers via `requests/auth.http` (not just unit tests with a
      mocked clock) for: an expired code (10+ min old, or `expires_at`
      backdated in dev DB), 6 wrong-code attempts hitting lockout, two
      resends within 60s hitting the cooldown, a 73-byte password hitting
      the explicit reject.
- [ ] `go test ./...` passes; `golangci-lint run --max-same-issues=0
      --max-issues-per-linter=0 ./...` clean.

## Non-goals

- No login endpoint (`CROC-006`) or forgot/reset password (`CROC-007`) —
  register + confirm only.
- No decision made here about whether `CROC-007`'s reset flow reuses the
  OTP-code shape — reset is a different UX context (user is actively on a
  "reset password" page, not reacting to a notification email), revisit
  at that ticket's own grill rather than assuming the same answer.
- No account linking for the Google-collision case — matches `CROC-002`'s
  standing "mutually exclusive, no linking" decision.
- `crockpot-react`'s `CFE-001`/`CFE-002` do not start until all of Epic 2
  (`CROC-005`–`009`) is done, per the corrected sequencing above.

## Verification modes

- **Logic with assertable behaviour**: `internal/handler/auth_handler_test.go`
  — failing test first, confirm red, then implement. `go test
  ./internal/handler/... ./internal/auth/...`.
- **Service/API boundary**: `internal/repository/*_test.go` against the
  real Neon dev `DATABASE_URL`; and a real Resend send (see AC) — email
  delivery can't be meaningfully proven by a mock, per this project's
  "real request against the real dependency" rule for this class of work.
- **Limits/config**: the 10-minute TTL, 5-attempt lockout, 60s resend
  cooldown, and 72-byte password cap are all new numbers being introduced
  here, not reused from precedent — each gets a real trigger through
  `requests/auth.http`, not just a unit test with a mocked clock (see AC).
- **Lint**: `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...`.
- **Code review**: `/code-review medium` on the full diff before
  close-out.

# CROC-008 — Refresh + logout

**Implementation mode: AI-driven.** Claude implements one piece at a time
(repository layer first, pause for review, then handler layer), per
`crockpot-go/CLAUDE.md`'s AI-driven cadence: failing test → minimal stub
that fails for the right reason → confirm red → stop for go-ahead →
implement → confirm green → stop before the next piece.

## Summary

`POST /auth/refresh` and `POST /auth/logout`, extending
`RefreshTokenRepository` with `RotateFamily`/`FindFamilyByID`/
`RevokeFamily` (already named in the master spec and in `CROC-004.md`/
`CROC-007.md`'s notes — this ticket is where they're actually built) and
adding rotate-on-use + reuse-detection, per the design
`docs/specs/master-spec.md` points at `packing-list-go`'s
`docs/handoffs/PACK-027.md` for.

**Starting position is already ahead of PACK-027's first draft**: this
project embedded `FamilyID` in the refresh JWT from `CROC-004` (see
`internal/auth/jwt.go`'s `RefreshClaims`) — PACK-027 originally shipped
without it (hash-only lookup) and had to reverse that decision
mid-implementation after live verification found stale-token replay
silently 401ing without revoking the live family. `FindFamilyByID` here
finds a family regardless of how stale the presented hash is, so that
specific bug class doesn't need re-discovering.

**Schema is already in place.** `refresh_tokens` (with
`previous_token_hash`/`previous_token_rotated_at`) and its
`idx_refresh_tokens_user_id` index were both added at `CROC-002`, ahead
of need — unlike PACK-027, which added its table fresh. No new migration
this ticket.

## Decisions from the interview

- **Grace window: 10 seconds**, same value and mechanism as PACK-027. A
  token matching `previous_token_hash` within 10s of
  `previous_token_rotated_at` is treated as a benign concurrent-refresh
  race (not reuse) and takes the same rotation path as a current-hash
  match — not distinguished in the response. Reused rather than
  re-derived: `crockpot-react`'s auth work hasn't started, so there's no
  frontend de-dupe behavior of its own to validate a different number
  against yet.
- **Reuse-detection branches** (locked in explicitly since PACK-027's own
  history shows this exact logic is where a real bug hid):
  - hash == `token_hash` → rotate
  - hash == `previous_token_hash` within grace window → rotate (same
    path)
  - hash matches neither, or family already has `revoked_at` set →
    `RevokeFamily`, generic 401
  - family not found by id → generic 401
- **Not-found convention: sentinel error, not `nil, nil`.** The AC below
  originally said `FindFamilyByID`/`UserRepository.FindByID` return
  `nil, nil` on a miss, sourced from PACK-027's convention without
  checking this repo's own. Corrected: every existing "find" method here
  (`FindByEmail`, `FindActiveByUserID`, `FindActiveByTokenHash`) returns
  a sentinel error via `errors.Is(err, pgx.ErrNoRows) →
  models.ErrXxx`, never `nil, nil` — `FindByID` reuses the existing
  `models.ErrUserNotFound` (same entity as `FindByEmail`), and
  `FindFamilyByID` gets a new `models.ErrRefreshTokenFamilyNotFound`,
  matching the naming pattern.
- **Access-token minting on refresh needs a new lookup.**
  `POST /auth/refresh` must return a fresh access token (per master
  spec: `GoogleCallback` mints no access token, the frontend gets its
  first one from `/auth/refresh` on load) — that needs the user's
  email+role for JWT claims, and `UserRepository` currently has no
  read-only by-id lookup (only `FindByEmail`, plus mutating
  `UpdateLastLogin`/`MarkEmailConfirmed`/`UpdateUserLoginProfile` which
  take an id but also write). **Add `UserRepository.FindByID`** — new
  `GetUserByID` sqlc query (mirrors existing `GetUserByEmail`), new
  interface method, read-only. Deliberately not reusing `UpdateLastLogin`
  for this: that would bump `last_login_at` on every silent access-token
  refresh (as often as every 15 min for an open tab), conflating "logged
  in" with "has an open tab" and corrupting that field for any future
  feature that reads it.
- **Refresh ordering: `FindByID` before `RotateFamily`.** Mirrors
  `CROC-006`'s own lesson (`LESSONS.md`, 2026-08-16: "the side effect
  that grants access goes last"). `RotateFamily` is what grants the
  continued session (new refresh cookie); if `FindByID` fails first
  (e.g. user row gone), nothing has mutated yet — no orphaned rotated
  family with a cookie the client can't pair with a valid access token.
- **Logout: decode-and-revoke directly, no repository lookup first.**
  Decode the refresh cookie's JWT to get `familyId`, call `RevokeFamily`
  directly — revoking a nonexistent/already-revoked family id is a
  harmless no-op `UPDATE`, so a `FindFamilyByID` lookup first would only
  add a second query for no behavioral difference. A missing, invalid,
  or expired cookie doesn't error — still clears the cookie and returns
  200, same as today's cookie-only behavior.
- **Error contract: single generic `401 invalid_refresh_token`** for
  every invalid-refresh case (missing cookie, expired, malformed,
  revoked, reuse-detected, family not found). Matches PACK-027 ("no
  product surface consumes a more specific signal") and this project's
  own `CROC-005` decision that validation-style failures collapse to one
  generic code, no field/reason-level detail.
- **Rate limits**: new `/auth/refresh`-only group at 30/min (matches the
  master spec's explicit "refresh 30/min" line — looser than login-type
  routes since a legitimate client calls it on every load/401, not just
  once per session). `/auth/logout` stays in the existing 10/min
  `authTight` group with register/login/etc — not called nearly as often
  as refresh, same abuse profile as the other one-shot auth routes, and
  the master spec names no separate number for it.
- **Sliding expiry, no absolute cap**: every `RotateFamily` call resets
  `expires_at` to `now + 7 days`, matching the "Session & revocation
  lifecycle" line in `docs/specs/master-spec.md`. No compliance driver
  for an absolute session cap, same reasoning PACK-027 used.
- **Cleanup: no new sweep in this ticket.** The lazy per-user sweep
  (`DeleteStaleFamiliesForUser`, called from `issueRefreshSession` on
  every new-session start) already exists from `CROC-004` and covers
  this — rotation overwrites a family's row in place, so it doesn't
  accumulate dead rows on its own; only genuinely abandoned/expired/
  revoked families need sweeping, and that already happens at next
  login.

## Acceptance criteria

**Repository layer (own commit, pause for review before the handler
layer):**

- [x] `RefreshTokenRepository` gains `RotateFamily(ctx, familyID,
      newTokenHash string, newExpiresAt time.Time) error` (shifts current
      `token_hash` into `previous_token_hash` + sets
      `previous_token_rotated_at`, sets new hash/expiry, one `UPDATE`),
      `FindFamilyByID(ctx, id, userID string) (*models.RefreshTokenFamily,
      error)` (returns `models.ErrRefreshTokenFamilyNotFound` if no row
      matches, per the corrected convention above), `RevokeFamily(ctx,
      familyID string) error` (sets `revoked_at`)
- [x] `PostgresRefreshTokenRepository` implements all three
- [x] `UserRepository` gains `FindByID(ctx, userID string) (*models.User,
      error)`; `PostgresUserRepository` implements it via a new
      `GetUserByID` sqlc query
- [x] Integration test confirms `RotateFamily` preserves the prior
      hash/timestamp in `previous_token_hash`/`previous_token_rotated_at`,
      not just overwriting silently
- [x] Integration test confirms `FindFamilyByID` returns the family for
      its own id/user, `models.ErrRefreshTokenFamilyNotFound` for an
      unknown id, and the same error for a correct id under the wrong
      user
- [x] Integration test confirms `RevokeFamily` sets `revoked_at`
- [x] Integration test confirms `UserRepository.FindByID` returns the
      user for a known id, `models.ErrUserNotFound` for unknown

**Handler layer:**

- [x] `POST /auth/refresh`: parses the refresh cookie via
      `auth.ValidateRefreshToken`; missing/invalid/expired token →
      `401 invalid_refresh_token`
- [x] Looks up the family by `claims.FamilyID` + `claims.Subject` via
      `FindFamilyByID`; not found, or found with `revoked_at` set →
      `401 invalid_refresh_token`
- [x] Hash matches `token_hash`, or matches `previous_token_hash` within
      the 10s grace window → `FindByID` for the user first; on failure,
      `500`, nothing mutated; on success, `RotateFamily`, set the new
      refresh cookie, mint and return a new access token,
      `200 {"accessToken": "..."}`
- [x] Hash matches `previous_token_hash` outside the grace window, or
      matches neither hash → `RevokeFamily`, `401 invalid_refresh_token`
- [x] A revoked family's token immediately fails on any subsequent
      `/auth/refresh` call, not just the triggering one — proven at the
      unit level (`TestRefreshToken_RejectsAlreadyRevokedFamily`) plus
      the repository integration test that `RevokeFamily` really
      persists `revoked_at`; full end-to-end proof (a real replay against
      a running server) is the outstanding manual verification pass below
- [x] `POST /auth/logout`: decodes the refresh cookie's `familyId` claim
      (if present and validly signed) and calls `RevokeFamily` directly —
      no lookup step. Missing/invalid/expired cookie doesn't error —
      still clears the cookie, `200`
- [x] `main.go`: new route group for `/auth/refresh` at 30/min;
      `/auth/logout` added to the existing 10/min `authTight` group;
      `NewAuthHandler` wiring unchanged (already takes
      `RefreshTokenRepository`)
- [x] `go tool mockery` re-run for the extended `RefreshTokenRepository`
      and `UserRepository` interfaces

## Non-goals

- No absolute session-lifetime cap — sliding window only
- No distinct wire-contract signal for reuse-detected vs. any other
  invalid-refresh-token case — both return the same generic 401
- No new/changed cleanup sweep — existing per-login lazy sweep from
  `CROC-004` covers this
- No `crockpot-react` changes — that project's auth work hasn't started
  (Epic 2 completing in full is the stated gate)
- No change to access-token generation/validation or the 15-minute
  access-token lifetime
- No new migration — `refresh_tokens`' shape and index were already
  added at `CROC-002`

## Expected test files

- `internal/repository/refresh_token_test.go` — extended:
  `TestRotateFamily_ShiftsCurrentIntoPrevious`,
  `TestFindFamilyByID_ReturnsFamily`,
  `TestFindFamilyByID_ReturnsErrRefreshTokenFamilyNotFoundForUnknownID`,
  `TestFindFamilyByID_ReturnsErrRefreshTokenFamilyNotFoundForWrongUser`,
  `TestRevokeFamily_SetsRevokedAt`
- `internal/repository/user_test.go` — extended: `TestFindByID_ReturnsUser`,
  `TestFindByID_ReturnsErrUserNotFound`
- `internal/handler/auth_handler_test.go` — extended:
  `TestRefreshToken_RotatesOnCurrentHash`,
  `TestRefreshToken_RotatesWithinGraceWindowOnPreviousHash`,
  `TestRefreshToken_RevokesOnStaleReuseOutsideGraceWindow`,
  `TestRefreshToken_RejectsAlreadyRevokedFamily`,
  `TestRefreshToken_RevokesOnMultiGenerationStaleReuse`,
  `TestRefreshToken_FailsBeforeRotatingWhenUserLookupFails`,
  `TestLogout_RevokesMatchingFamily`,
  `TestLogout_ClearsCookieEvenWithoutValidRefreshToken`

## Manual verification (`requests/auth.http`)

**Corrected at implementation time**: `CLAUDE.md`'s original plan here
(seed a `DEV_REFRESH_TOKEN` env var by hand from a real browser
Google-login cookie) predates `Login` existing in this file. Since
`Login` (`CROC-006`) already sets a real `refreshToken` cookie via
`Set-Cookie`, and REST Client's cookie jar carries it forward
automatically to later requests in the same run, the `refresh`/`logout`
sections chain directly off `Login`'s cookie instead — no manual browser
round-trip needed, and CROC-008's rotation/reuse/revoke mechanism is
identical regardless of which login method created the family.
`CLAUDE.md`'s "Token acquisition" section is updated to match.

Sequence, run right after the existing `Login` section: refresh (rotate,
capture new cookie) → refresh again (rotate again, proves rotation) →
replay the *original* pre-rotation cookie (copied by hand from the
Login response's `Set-Cookie` header before the first refresh) after the
10s grace window has passed (expect 401, family revoked) → refresh with the latest cookie (expect 401, proves the whole
family is dead, not just the replayed token) → separately, `POST
/auth/logout` on a fresh session → refresh (expect 401, proves
server-side revocation, not just cookie clearing). Also exercises the
30/min rate limit and the 10s grace-window boundary for real (real
timing, real repeated calls), not just against a mocked clock in a unit
test.

## Close-out

Repository and handler layers implemented, both TDD (red confirmed
before each layer's real bodies), all green: `go test ./internal/handler/...`,
`./scripts/test-repo.sh`, `go build ./...`, `go vet ./...`, `gofmt -l .`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
(0 issues), `go mod tidy -diff` (no changes — no new dependencies this
ticket). `go tool mockery` re-run for the extended interfaces.

`requests/auth.http` extended with the refresh/logout sections (rotate ×2,
deliberate stale-replay, whole-family-dead check, then a separate
logout → post-logout-replay sequence, plus a note on manually exercising
the 30/min rate limit) — chains off `Login`'s cookie via REST Client's
jar rather than the `DEV_REFRESH_TOKEN` env var `CLAUDE.md` originally
sketched (corrected in both the "Manual verification" section above and
`CLAUDE.md` itself). `requests/README.md` updated to match.

Manual verification run against a real server: rotation confirmed live.
The first stale-replay attempt returned 200 rather than the expected 401
— traced to not having waited the 10s grace window, not a bug (a
`previous_token_hash` match within the window is deliberately treated as
a benign concurrent-refresh race and rotates normally, per this ticket's
own grace-window decision). Founder called this gate complete on that
basis rather than re-running the full timed replay sequence.

CodeRabbit's review on the PR found 3 real issues, all fixed:

- **Doc**: this file's "Manual verification" section claimed the
  original pre-rotation cookie was saved via REST Client's `@name`
  syntax — it isn't, `requests/auth.http` always used a manual
  copy-paste into a placeholder. Wording corrected.
- **`Logout` swallowed a `RevokeFamily` failure and still returned 200.**
  A presented, valid token whose revoke genuinely fails now returns
  `500 server_error` (cookie still cleared either way) instead of
  silently reporting success while the family stays live server-side.
  New test: `TestLogout_ReturnsErrorWhenRevocationFails`.
- **`RotateFamily`'s `UPDATE ... WHERE id = $1` had a TOCTOU race.**
  The presented-hash/grace-window decision was made in Go against an
  earlier read, then written unconditionally — a concurrent rotation
  landing in between could let a stale token "win" a rotation instead of
  triggering revocation. Fixed by moving the hash/grace-window check
  into the `UPDATE`'s `WHERE` clause itself (`internal/sqlc/queries/refresh_tokens.sql`,
  now `:execrows`), re-validated atomically against the live row at
  write time. `RotateFamily`'s signature changed to
  `(ctx, familyID, presentedHash, newTokenHash string, newExpiresAt,
  graceWindowCutoff time.Time) (bool, error)` — a `false` return (zero
  rows affected) means the hash no longer qualified at write time, and
  the handler now revokes on that signal instead of on a Go-side branch.
  This also collapses `RefreshToken`'s four Go-side branches (current
  hash / previous-within-grace / previous-outside-grace / matches
  neither) down to two (rotated / not rotated) — the hash/grace-window
  distinction itself now lives entirely in the SQL, tested by 5 new
  repository tests (`TestRotateFamily_Succeeds*`/`Fails*`) rather than
  in handler-level mocks. `internal/handler/auth_handler_test.go`'s
  Refresh tests were rewritten to match — the old
  `RotatesOnCurrentHash`/`RotatesWithinGraceWindowOnPreviousHash`/
  `RevokesOnStaleReuseOutsideGraceWindow`/`RevokesOnMultiGenerationStaleReuse`
  tests collapsed into `RotatesWhenAtomicRotateSucceeds`/
  `RevokesWhenAtomicRotateReportsNoMatch`, plus a new
  `FailsWhenAtomicRotateErrors` distinguishing a genuine DB error from an
  atomic no-match (must not be treated as reuse).

All green after the fix: `go build ./...`, `go vet ./...`, `gofmt -l .`,
`go test ./internal/handler/...`, `./scripts/test-repo.sh`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
(0 issues), `go mod tidy -diff` (no changes). `go tool mockery` re-run
for the changed `RefreshTokenRepository` signature.

Completed 2026-08-17.

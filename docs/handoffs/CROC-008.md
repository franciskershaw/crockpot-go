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

- [ ] `RefreshTokenRepository` gains `RotateFamily(ctx, familyID,
      newTokenHash string, newExpiresAt time.Time) error` (shifts current
      `token_hash` into `previous_token_hash` + sets
      `previous_token_rotated_at`, sets new hash/expiry, one `UPDATE`),
      `FindFamilyByID(ctx, id, userID string) (*models.RefreshTokenFamily,
      error)` (nil, nil if no row matches — mirrors existing not-found
      convention), `RevokeFamily(ctx, familyID string) error` (sets
      `revoked_at`)
- [ ] `PostgresRefreshTokenRepository` implements all three
- [ ] `UserRepository` gains `FindByID(ctx, userID string) (*models.User,
      error)`; `PostgresUserRepository` implements it via a new
      `GetUserByID` sqlc query
- [ ] Integration test confirms `RotateFamily` preserves the prior
      hash/timestamp in `previous_token_hash`/`previous_token_rotated_at`,
      not just overwriting silently
- [ ] Integration test confirms `FindFamilyByID` returns the family for
      its own id/user, nil for an unknown id, nil for a correct id under
      the wrong user
- [ ] Integration test confirms `RevokeFamily` sets `revoked_at`
- [ ] Integration test confirms `UserRepository.FindByID` returns the
      user for a known id, nil for unknown

**Handler layer:**

- [ ] `POST /auth/refresh`: parses the refresh cookie via
      `auth.ValidateRefreshToken`; missing/invalid/expired token →
      `401 invalid_refresh_token`
- [ ] Looks up the family by `claims.FamilyID` + `claims.Subject` via
      `FindFamilyByID`; not found, or found with `revoked_at` set →
      `401 invalid_refresh_token`
- [ ] Hash matches `token_hash`, or matches `previous_token_hash` within
      the 10s grace window → `FindByID` for the user first; on failure,
      `500`, nothing mutated; on success, `RotateFamily`, set the new
      refresh cookie, mint and return a new access token,
      `200 {"accessToken": "..."}`
- [ ] Hash matches `previous_token_hash` outside the grace window, or
      matches neither hash → `RevokeFamily`, `401 invalid_refresh_token`
- [ ] A revoked family's token immediately fails on any subsequent
      `/auth/refresh` call, not just the triggering one
- [ ] `POST /auth/logout`: decodes the refresh cookie's `familyId` claim
      (if present and validly signed) and calls `RevokeFamily` directly —
      no lookup step. Missing/invalid/expired cookie doesn't error —
      still clears the cookie, `200`
- [ ] `main.go`: new route group for `/auth/refresh` at 30/min;
      `/auth/logout` added to the existing 10/min `authTight` group;
      `NewAuthHandler` wiring unchanged (already takes
      `RefreshTokenRepository`)
- [ ] `go tool mockery` re-run for the extended `RefreshTokenRepository`
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
  `TestFindFamilyByID_ReturnsNilForUnknownID`,
  `TestFindFamilyByID_ReturnsNilForWrongUser`,
  `TestRevokeFamily_SetsRevokedAt`
- `internal/repository/user_test.go` (or wherever `FindByEmail`'s test
  lives) — extended: `TestFindByID_ReturnsUser`,
  `TestFindByID_ReturnsNilForUnknownID`
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

Extended per `crockpot-go/CLAUDE.md`'s already-documented mechanism:
`DEV_REFRESH_TOKEN` seeded once by hand from a real browser Google-login
cookie, chained via `{{$dotenv DEV_REFRESH_TOKEN}}`. Sequence: refresh
(rotate, capture new cookie) → refresh again (rotate again, proves
rotation) → replay the original `DEV_REFRESH_TOKEN` after the 10s grace
window has passed (expect 401, family revoked) → refresh with the latest
cookie (expect 401, proves the whole family is dead, not just the
replayed token) → separately, a fresh login → `POST /auth/logout` →
refresh (expect 401, proves server-side revocation, not just cookie
clearing). Also exercises the 30/min rate limit and the 10s grace-window
boundary for real (real timing, real repeated calls), not just against a
mocked clock in a unit test.

## Close-out

Not started.

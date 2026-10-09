# CROC-069 — Auth identity hardening

Emails become case-insensitive identities: stored lowercase, enforced by a
CHECK, lowercased by the repository on every lookup and insert. The
confirmation code's 5-attempt cap becomes exact under concurrent guesses.
Must land before the prod import (`CROC-077`). Grilled 2026-10-09.
Expensive to undo: it fixes the identity rule real accounts are created
under. Source: `docs/findings/2026-10-04-tech-debt.md` findings 2–3.

**Implementation mode**: AI-driven.

## Facts this rests on (checked 2026-10-09)

- `users.email` is `TEXT UNIQUE NOT NULL` (`000001_init.up.sql:8`), so
  the constraint is `users_email_key`. Lookups are `WHERE email = $1`
  (`internal/sqlc/queries/users.sql:12`).
- `user.go:47` (`GetOrCreateUser`) and `user.go:65`
  (`CreateUnconfirmedUser`) map conflicts by matching
  `ConstraintName == "users_email_key"`.
- Every email lookup and insert goes through three repository methods:
  `FindByEmail`, `CreateUnconfirmedUser`, `GetOrCreateUser`. Callers:
  `Register`, `ConfirmEmail`, `ResendConfirmation`, `Login`,
  `ForgotPassword` (`binding:"required,email"`), and `GoogleCallback`
  (`idTokenClaims.Email`, `auth_handler.go:223`).
- Whitespace never reaches a handler: `binding:"email"` rejects it, and
  the frontend trims (`crockpot-react` `authSchemas.ts:4`).
- `cmd/migrate-data` inserts users through its own queries
  (`load.go:161`), with the email taken from `transform.go:286`.
- The export (`../crockpotV3.User.json`) has 42 users, 0 mixed-case, 0
  padded, 0 case-duplicates. Every repo-test fixture builds a lowercase
  email (`"repo-test-" + uuid + "@example.com"`). The dev DB is unchecked
  (no psql).
- `ConfirmEmail` (`auth_handler.go:355`) reads `token.Attempts`, checks
  expiry, compares the hash, then calls `IncrementAttempts`, an
  unconditional `attempts + 1`. `ConfirmEmail` is its only caller. The
  frontend distinguishes `code_expired` and `too_many_attempts`
  (`crockpot-react` `authErrors.ts:32,39`).

## Decisions

1. **Store lowercase and enforce with a CHECK. No `lower(email)` index,
   no `citext`.** Migration `000018` lowercases existing rows and adds
   `CONSTRAINT users_email_lowercase CHECK (email = lower(email))`.
   `users_email_key` stays, so every query and both constraint-name
   matches stay valid. The CHECK makes a write path that forgets to
   normalise fail loudly instead of creating a duplicate. Gives up
   display casing. Rejected: a `lower(email)` index (every lookup
   rewritten, constraint name changes, stored value differs from the
   key) and `citext` (extension plus a sqlc override for what the CHECK
   already guarantees). Down migration drops the CHECK only; the
   lowercasing isn't reversed.
2. **Normalise in the repository, not the handlers.** `FindByEmail`,
   `CreateUnconfirmedUser` and `GetOrCreateUser` lowercase their `email`
   argument through one `normaliseEmail` helper in `repository/`. The
   CHECK guards writes only; a lookup that missed normalisation would
   fail silently ("invalid credentials"), so lookups need the single
   choke point. Handler tests (mocked repo) can't see it; repo tests
   prove it. No `TrimSpace` (see facts).
3. **Claim an attempt atomically before comparing.** `ConfirmEmail`
   order: find token → existing `Attempts >= 5` fast path →
   expiry → `ClaimAttempt` → hash compare. `ClaimAttempt` replaces
   `IncrementAttempts`: `UPDATE … SET attempts = attempts + 1 WHERE id =
   $1 AND attempts < $2 RETURNING attempts`; no row returns
   `models.ErrTooManyAttempts` → `400 too_many_attempts`. A correct guess
   now spends an attempt too, which is harmless (the token is then
   marked used; a correct 5th guess claims 4→5 and succeeds). Every
   existing response is unchanged. Rejected: `SELECT … FOR UPDATE` in a
   transaction (same guarantee, more code).
4. **`migrate-data` lowercases in `transform.go`.** The CHECK would
   otherwise be first met mid-cutover with the old site offline.

## Acceptance criteria

- [ ] Migration `000018` up on the dev DB: existing emails lowercased,
      `users_email_lowercase` present; down/up round-trips.
- [ ] A raw `INSERT` of a mixed-case email fails with SQLSTATE `23514`
      on `users_email_lowercase`.
- [ ] `CreateUnconfirmedUser("Jane@Example.com")` stores
      `jane@example.com`; `FindByEmail("JANE@example.com")` finds it.
- [ ] `CreateUnconfirmedUser` with a case variant of an existing
      account returns the same sentinel as an exact match
      (`ErrEmailRegisteredWithPassword` / `WithGoogle` / `Unconfirmed`).
- [ ] `GetOrCreateUser` with a mixed-case email matching a password
      account returns `ErrEmailRegisteredWithPassword`; a new Google user
      is stored lowercase.
- [ ] `ClaimAttempt`: 20 concurrent claims on one token produce exactly
      5 successes and 15 `ErrTooManyAttempts`; seen failing against the
      unconditional increment.
- [ ] `ConfirmEmail`: a failed claim returns `400 too_many_attempts`
      without comparing; wrong and correct codes both claim first;
      expired still returns `code_expired` without claiming.
- [ ] `migrate-data` transform lowercases a mixed-case source email.
- [ ] Mocks regenerated (`go tool mockery`); `IncrementAttempts` and its
      query removed.
- [ ] `requests/auth.http`: register in mixed case, then log in with the
      lowercase form.

## Non-goals

- Display casing, `citext`, or a `lower()` index.
- Server-side trimming.
- An email-change feature (still out, per `CROC-051`).
- `CROC-071`'s `pgerror.go` retrofit of `user.go`.
- Per-code lockout beyond the existing 5 attempts / resend.

## Pieces

1. **Migration + schema test**: `000018`, CHECK test (criteria 1–2). Red,
   stop, green, stop.
2. **Repository normalisation**: `normaliseEmail`, three methods, repo
   tests (criteria 3–5).
3. **Attempt claim**: query, `ClaimAttempt`, sentinel, concurrency test,
   `ConfirmEmail` change, handler tests, mock regen (criteria 6, 7, 9).
4. **migrate-data**: transform lowercase + transform test (criterion 8).
5. **`.http`** walkthrough (criterion 10).

## Verification

- **Logic** (failing tests first): `./scripts/test-repo.sh -run
  'Email|User|ClaimAttempt'`, `go test ./internal/handler/...`,
  `go test ./cmd/migrate-data/...`.
- **Service boundary**: the migration applied to the Neon dev DB by
  starting the server, then `requests/auth.http` run against the local
  API (`lsof -iTCP:8080 -sTCP:LISTEN` first): register `Mixed@…`,
  confirm, log in as `mixed@…`. A case-duplicate in the dev DB fails the
  migration loudly; delete the duplicate and rerun.
- Full repo suite green against Neon before close-out; lint uncapped.

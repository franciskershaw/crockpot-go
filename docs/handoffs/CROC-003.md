# CROC-003 — JWT helpers + auth middleware

**Implementation mode: hand-written.** This ticket is being implemented
by the founder directly, referencing `packing-list-go`'s actual files
(`internal/auth/jwt.go`, `internal/middleware/auth.go`) plus this doc —
not by Claude. Unlike `CROC-001`, this ticket **is** test-first: the
founder writes both the tests and the implementation by hand, red before
green. See `CLAUDE.md`'s "Overrides of the global default process" for
what hand-written mode does and doesn't skip — Claude still runs a
`code-review` pass on the diff once it's written, and checks the
verification evidence below, before this closes out.

## Context

Give the app a way to issue and validate its own signed session tokens,
and a piece of middleware that gates a route behind one — the mechanism
every later protected endpoint (`CROC-004` on) will sit behind. No
protected route exists yet; this ticket only builds the mechanism.

## Decisions and why

Claims checked against real source before any of this was decided:

- **`packing-list-go/internal/auth/jwt.go` and
  `internal/middleware/auth.go`** — read both directly. `CustomClaims{Email,
  UserID, jwt.RegisteredClaims}` for access tokens (HS256, 15-min
  expiry), `RefreshClaims{FamilyID, jwt.RegisteredClaims}` for refresh
  (7-day expiry) — `FamilyID` already matches exactly what `CROC-002`'s
  `refresh_tokens.id` was built for (`PACK-027`'s family-id-in-JWT
  design). `AuthMiddleware` reads `Authorization`, splits `"Bearer
  <token>"`, validates, sets `userID`/`email` in Gin context, generic
  `401` on anything wrong.
- **No original design doc exists for these two files.** `PACK-005.md`
  (the only handoff doc mentioning JWT/auth) is explicitly retroactive
  and only covers the `/auth/refresh`/`/auth/logout` *handlers*, not the
  original creation of `jwt.go`/`auth.go`. "Mirrors `packing-list-go`'s
  design" means the code itself, already read directly above — there's
  no additional rationale doc to cross-check nuance against.
- **A real gap in the reference, not inherited here.**
  `packing-list-go/internal/middleware/` has test files for `cors.go`,
  `body_limit.go`, `error_logger.go`, `rate_limit.go` — but no test file
  for `auth.go` at all, despite it being the highest-stakes middleware in
  that app. `jwt_test.go` itself covers generate/validate round-trip and
  empty-secret errors, but nothing for an expired token or a
  tampered/invalid-signature token.

**This ticket is test-first, unlike `CROC-001`'s `db.go`/`main.go`.**
`CROC-001`'s own stated boundary: "any future ticket that adds real
branching logic... should write tests for that delta, not extend this
exemption." This ticket *is* that real branching logic (token
expiry/signature/format checks), so it doesn't inherit `CROC-001`'s
no-tests pass. Per `packing-list-go/CLAUDE.md`'s "TDD in Go": write the
test first, a minimal stub that fails for the right reason (not a
panic), confirm every test fails before implementing, then make it pass.

**`CustomClaims` gains a `Role` field — `packing-list-go` has no
analogue.** That project has no tiers at all, so its `CustomClaims`
never had to consider this. Crockpot's `users.role`
(`FREE`/`PREMIUM`/`PRO`/`ADMIN`, `CROC-002`) plus master-spec's `Epic 7`
(dedicated to tier-gating) means the access-token claims shape has to
make a call the reference project's file structurally cannot answer.
**Decision: embed it** — `Epic 7`'s gating middleware checks
`claims.Role` directly, no DB round-trip on gated requests, no
`UserRepository` dependency in that middleware. Accepted trade-off: a
role change takes up to one access-token cycle (15 min) to take effect —
matches `PACK-027`'s "no compliance driver for this personal app"
reasoning for a similar staleness call elsewhere in this stack. Recorded
in master-spec's "Key architecture decisions" (Auth bullet) since this
outlives the ticket. `GenerateAccessToken`'s signature grows a
parameter as a direct consequence: `GenerateAccessToken(email, userID,
role, secret string)`.

**Two test cases added beyond what `packing-list-go` covers**: expired
token and tampered signature, for both access and refresh tokens. Cheap
to add (pure functions, no DB, no server) and exactly the failure modes
that matter most for a JWT validator in production — see "the gap" above.

**Middleware's `401` response stays generic**, matching
`packing-list-go`'s single "invalid token" message for both expired and
tampered cases — no need for the wire response to distinguish them.
`PACK-027` already established the frontend precedent: any `401`
triggers a refresh attempt uniformly, not a specific
expired-vs-invalid branch. The distinction only needs to exist at the
**test** level (proving both failure modes are actually caught), not in
what the client sees.

**Service/API boundary verification deliberately doesn't happen in this
ticket.** No protected route exists yet to run the middleware against
for real — that proof lands with `CROC-009` (`GET /me`, the first
authenticated route), noted on its own backlog entry so it isn't
rediscovered as a surprise (same "first real consumer proves it" pattern
`CROC-002` used for deferring `sqlc generate` to `CROC-004`).

## Acceptance criteria

- [x] `github.com/golang-jwt/jwt/v5@v5.3.1` and `github.com/google/uuid@v1.6.0`
      added to `go.mod`, matching `packing-list-go`'s pins. (Both landed
      as `// indirect` mid-ticket since nothing had imported them yet at
      that point; `go mod tidy` corrected this once `jwt.go` was written
      and importing both directly — caught during the AC pass, not
      missed.)
- [x] `internal/auth/jwt.go`: `CustomClaims{Email, UserID, Role string;
      jwt.RegisteredClaims}`, `RefreshClaims{FamilyID string;
      jwt.RegisteredClaims}` — structurally identical to
      `packing-list-go/internal/auth/jwt.go` except `Role`.
- [x] `GenerateAccessToken(email, userID, role, secret string) (string,
      error)` — HS256, 15-min expiry, empty secret returns an error
      (never panics).
- [x] `GenerateRefreshToken(userID, familyID, secret string) (string,
      error)` — HS256, 7-day expiry, `familyID` embedded as the
      `familyId` claim, empty secret returns an error.
- [x] `ValidateAccessToken(tokenString, secret string) (*CustomClaims,
      error)` / `ValidateRefreshToken(tokenString, secret string)
      (*RefreshClaims, error)` — reject wrong signing method, wrong
      secret, expired token, tampered token; empty secret returns an
      error.
- [x] `internal/middleware/auth.go`: `AuthMiddleware(secret string)
      gin.HandlerFunc` — missing `Authorization` header → `401`;
      malformed header (no `Bearer` prefix, wrong part count) → `401`;
      invalid/expired token → `401`; valid token → sets `userID`/`email`
      in Gin context, calls `c.Next()`. (Also sets `role` in context —
      not in this AC, added ahead of `Epic 7`'s tier-gating middleware
      that will need it; harmless superset, not a deviation.)
- [x] `internal/auth/jwt_test.go`: `TestGenerateAccessToken` (round-trip,
      including asserting `claims.Role`), `TestGenerateRefreshToken`
      (round-trip incl. `FamilyID`), `TestGenerateAccessToken_EmptySecret`,
      `TestGenerateRefreshToken_EmptySecret`,
      `TestValidateAccessToken_EmptySecret`,
      `TestValidateRefreshToken_EmptySecret`,
      `TestValidateAccessToken_ExpiredToken`,
      `TestValidateAccessToken_TamperedSignature`,
      `TestValidateRefreshToken_ExpiredToken`,
      `TestValidateRefreshToken_TamperedSignature`.
- [x] `internal/middleware/auth_test.go` (new — no reference file to
      copy from): one `TestAuthMiddleware`, table-driven over the four
      rejection cases (`missing header`, `malformed header`,
      `invalid token`, `expired token`) plus a standalone
      `valid token sets context` subtest — five cases via `t.Run`, not
      five top-level functions as originally drafted here. Chosen once
      the founder was mid-implementation: the four rejection cases share
      an identical shape (build a request, assert `401` + handler not
      called), which is exactly the case `t.Run` tables are for; the
      valid-token case stayed separate since it asserts `200` +
      context values, a genuinely different shape.
- [x] `go test ./internal/auth/... ./internal/middleware/...` passes,
      every test failing for the right reason before implementation
      (confirmed red), then passing (confirmed green).

## Roadmap (hand-write order)

Test-first throughout — the inverse of `CROC-001`'s roadmap, which had
no TDD-stub step. Each piece: write the failing test(s), confirm red for
the right reason (not a panic aborting the whole binary), then implement
until green.

0. **Check prerequisites.** Confirm `CROC-002` is on `main`
   (`refresh_tokens.id` is what `FamilyID` will carry — no hard
   dependency for compiling this ticket's code, but the shape needs to
   already make sense against it). `go version` matches `CROC-001`'s.
1. **Add dependencies.** `go get github.com/golang-jwt/jwt/v5@v5.3.1
   github.com/google/uuid@v1.6.0`.
2. **Write `internal/auth/jwt_test.go` first**, all ten cases from the
   AC list above. Reference `packing-list-go/internal/auth/jwt_test.go`
   for the six it already covers (adapt: assert `claims.Role` too on the
   access-token round-trip test) — the four new ones (expired, tampered,
   ×2 token types) have no reference to copy, write them from the AC
   description. Run `go test ./internal/auth/...` and confirm every test
   fails — compile error is fine at this stage (no `jwt.go` yet), but if
   it compiles and a test panics instead of failing an assertion, fix the
   stub before moving on.
3. **Write `internal/auth/jwt.go`** against `packing-list-go`'s file,
   with the one deliberate delta: `Role` added to `CustomClaims`, threaded
   through `GenerateAccessToken`'s new parameter. Run `go test
   ./internal/auth/...`, confirm all ten pass.
4. **Write `internal/middleware/auth_test.go` first** — no reference file
   exists, build it from the AC list's five cases. Use `httptest` +
   `gin.New()` per this project's black-box-handler-test convention
   (`CLAUDE.md`/master-spec's architecture section) — no repository
   involved, so no `testify/mock` needed here, just a real `gin.Context`
   and a real (test-generated) token from step 3's `jwt.go`. Confirm all
   five fail for the right reason before writing the middleware.
5. **Write `internal/middleware/auth.go`** — direct port of
   `packing-list-go/internal/middleware/auth.go`, no deltas. Run `go test
   ./internal/middleware/...`, confirm all five pass.
6. **Run the full ticket's test scope together**: `go test
   ./internal/auth/... ./internal/middleware/...`, confirm all fifteen
   pass.
7. **Lint**: `golangci-lint run --max-same-issues=0
   --max-issues-per-linter=0 ./...`, fix anything it flags.
8. **Hand off** — ping for the `code-review` pass named in this doc's
   header before calling `CROC-003` closed.

## Non-goals

- No protected route — `AuthMiddleware` is built but not wired onto any
  real endpoint yet (see "Service/API boundary" note above; that's
  `CROC-009`).
- No refresh-token rotation/reuse-detection logic — this ticket only
  generates/validates the JWTs; the rotation state machine against
  `refresh_tokens` is `CROC-008`.
- No tier-gating middleware itself — this ticket only makes `role`
  available on the claims; consuming it to actually gate a route is
  `Epic 7`.

## Verification modes

- **Logic with assertable behaviour** (the primary mode for this
  ticket): failing test first, minimal stub, confirm red, then
  implement — see Roadmap. `go test ./internal/auth/...
  ./internal/middleware/...`.
- **Limits/config**: the 15-min/7-day expiry values are fixed by
  master-spec already (matching `packing-list-go`), not new numbers
  being tuned here — exercised via `TestValidateAccessToken_ExpiredToken`/
  `TestValidateRefreshToken_ExpiredToken`, which run a manufactured
  past-`ExpiresAt` claim through the real `jwt` library's real expiry
  check, not a mocked clock.
- **Service/API boundary**: deliberately not exercised in this ticket —
  no protected route exists yet. Proof lands with `CROC-009` (see
  `docs/specs/master-spec.md`'s `CROC-009` entry).
- **Lint**: `golangci-lint run --max-same-issues=0
  --max-issues-per-linter=0 ./...` — run for real.
- **Code review**: once hand-written, Claude runs `/code-review` on the
  diff before close-out.

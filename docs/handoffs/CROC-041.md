# CROC-041 — Unify the API error-response shape onto shared helpers

**Implementation mode: AI-driven.** Behaviour-preserving refactor — there
is no new assertable behaviour, so the normal "failing test → stub →
confirm red" step does not apply. Each piece is: refactor → run the
existing suite → confirm still green → stop for review. The one
intentional behaviour change (`rate_limit.go`'s two codes) updates its
existing test assertions in the same piece.

## Summary

Add five void error-response helpers next to `internalError` in
`handler/errors.go`, then retrofit every hand-written
`c.JSON(<status>, gin.H{"error": <code>})` site in the `handler` package
onto them — the six CRUD handlers, `validation.go`, **and**
`auth_handler.go` (which also adopts `bindJSON` and `internalError`,
absorbing tech-debt finding 8). Separately, fix
`middleware/rate_limit.go`'s two sentence-style bodies to snake_case
codes (finding 6).

Raised at `CROC-014` (handoff doc, piece 3): the recipe handler's
validation layer made the per-line `c.JSON(http.StatusBadRequest,
gin.H{"error": ...})` noise obvious, and CROC-015–018 will copy it
further if left. Classified **cheap-to-undo** at grill time — same
status codes, same bodies (bar the two rate-limit codes), no contract
/ schema / endpoint change, reversible by reverting the diff. Recorded
as a handoff doc anyway: it spans two packages and ~11 files, absorbs
another ticket's scope, and every ticket to date has one.

## Decisions from the interview

### 1. Absorbs tech-debt findings 8 and 6; `CROC-036` shrinks to finding 7

- **Finding 8** (`auth_handler.go` never retrofitted onto the shared
  helpers) is the *same concern* as this ticket's core and is most of
  `CROC-036`'s actual work. Doing the whole `auth_handler.go` retrofit
  here — error helpers **plus** `bindJSON` **plus** `internalError`, in
  one careful pass — is less work than two passes and removes the
  "coherent unit" reason for keeping it separate.
- **Finding 6** (`rate_limit.go`'s `"rate limit exceeded"` /
  `"internal server error"` bodies) is a thematically identical 2-line
  fix — folded in.
- **Finding 7** (`db.go`'s `fmt.Print*` → `slog`) is a genuinely
  separate logging concern in the `db` package — stays as `CROC-036`,
  now that ticket's only finding.
- The findings doc (`docs/findings/2026-08-30-tech-debt.md`) and the
  `CROC-036` / `CROC-041` spec bullets were updated alongside this
  handoff.

### 2. Helper API — one void function per status class, mirroring `internalError`

In `handler/errors.go`, next to `internalError`:

```go
func badRequest(c *gin.Context, code string)   { c.JSON(http.StatusBadRequest, gin.H{"error": code}) }
func notFound(c *gin.Context, code string)     { c.JSON(http.StatusNotFound, gin.H{"error": code}) }
func conflict(c *gin.Context, code string)     { c.JSON(http.StatusConflict, gin.H{"error": code}) }
func unauthorized(c *gin.Context, code string) { c.JSON(http.StatusUnauthorized, gin.H{"error": code}) }
func forbidden(c *gin.Context, code string)    { c.JSON(http.StatusForbidden, gin.H{"error": code}) }
```

- **Void, like `internalError`.** Call sites keep their explicit
  `return`: `badRequest(c, "invalid_time"); return`. No
  `return badRequest(...)`.
- **Five named helpers, not a generic `errorResponse(c, status, code)`.**
  The ticket's purpose is call-site noise reduction and one idiom;
  `badRequest(c, "code")` is the shortest thing that reads clearly. A
  generic helper keeps the `http.Status*` constant at every call site —
  barely shorter than the status quo. Five three-line functions matches
  the existing `internalError` precedent in the same file.
- `unauthorized` / `forbidden` are included (not just the three the
  backlog line named) because `auth_handler.go` and
  `recipe_handler.go:33` need them and they are the identical shape.
- `serverError(c)` was added in piece 3 (see the `auth_handler.go`
  guards) — the generic 500 body with no error-wrapping;
  `internalError` now calls it.
- **Helpers ship with their first consumer, not in a standalone commit** —
  golangci-lint's `unused` linter flags an unreferenced package-level
  func. So piece 1 adds only `badRequest` / `notFound` / `conflict`
  (all consumed by the reference-data handlers); `unauthorized` lands
  in piece 2 (first use: `recipe_handler.go:33`); `forbidden` lands in
  piece 3 (first use: `auth_handler.go`).

### 3. What gets retrofitted, per file

| File | Sites | Notes |
| --- | --- | --- |
| `item_category_handler.go` | 8 | `badRequest` / `notFound` / `conflict` |
| `item_handler.go` | 7 | same |
| `unit_handler.go` | 8 | same |
| `recipe_category_handler.go` | 5 | same |
| `recipe_handler.go` | 7 | incl. `unauthorized(c, "unauthorized")` at line 33 |
| `recipe_requests.go` | 21 | all `badRequest` |
| `validation.go` | 9 | its own internal `c.JSON(400, ...)` sites call `badRequest` |
| `auth_handler.go` | ~60 | 21 `badRequest`, 8 `unauthorized`, 2 `conflict`, 1 `forbidden`; 28 `StatusInternalServerError` → `internalError`; 6 `ShouldBindJSON` → `bindJSON` |
| `middleware/rate_limit.go` | 2 | **string change only** — `rate_limit_exceeded` / `server_error`, inline (middleware can't import `handler`) |

**`auth_handler.go` guards:**
- **`internalError` only where there is a preceding
  `c.Error(fmt.Errorf("...: %w", err))` with a real error value.** The
  existing wrap message becomes the `logMsg` arg — the produced string
  (`internalError` wraps as `"%s: %w"`) must be unchanged.
- **`serverError(c)` (new helper) for the two sites where the error is
  already fully wrapped before the `_ = c.Error(...)` — Login's
  `issueRefreshSession` failure and ResetPassword's `txErr`. Feeding
  those to `internalError` would double the message prefix. `internalError`
  itself now delegates its `c.JSON` to `serverError`.** A bare
  `c.JSON(500, "server_error")` with no error at all would also use
  `serverError`; there turned out to be none.
- **`bindJSON`** replaces the `c.ShouldBindJSON(&req)` + inline
  `c.JSON(400, gin.H{"error": "invalid_request"})` + `return` triplet
  only where it matches exactly.
- **Do not touch `GoogleCallback`'s `?error=<code>` redirect path**
  (`http.StatusTemporaryRedirect`, 3 sites) — deliberate exception per
  `master-spec.md` (top-level navigation target, not a JSON consumer).
- **Do not touch the `429` bodies** — `resend_too_soon` /
  cooldown responses carry an extra `retryAfterSeconds` field, not the
  generic shape (3 `StatusTooManyRequests` sites). Stay hand-written.
- **Do not force-fit `validateName`** — `auth_handler.go` validates
  email / password bounds, not entity names; it has no `validateName`
  call to make.

### 4. Out of scope

- `middleware/auth.go` (`"missing authorization header"`, `"invalid
  authorization header"`, `"invalid token"`) and `middleware/role.go`
  (`"forbidden"`) — different package, not flagged by any tech-debt
  finding, sentence-style bodies left for a future pass. Only
  `rate_limit.go` (finding 6) is in scope in `middleware`.
- `db.go` → `slog` — stays `CROC-036` (finding 7).
- Any status-code, response-body, or redirect change other than
  `rate_limit.go`'s two codes.
- `requests/*.http` — no endpoint contract changes, nothing to update.

## Non-goals

- Field-level validation detail on `invalid_request` — `master-spec.md`
  decision, unchanged.
- Consolidating the `handler` and `middleware` error idioms into one
  shared package — `middleware` can't import `handler`; not worth a
  third location for this.
- Touching `internalError`'s signature or behaviour.

## Acceptance criteria

- [ ] `handler/errors.go` defines `badRequest`, `notFound`, `conflict`,
      `unauthorized`, `forbidden` — each a one-line
      `c.JSON(<status>, gin.H{"error": code})`, void, no return value.
- [ ] Every `c.JSON(http.StatusBadRequest|StatusNotFound|StatusConflict|
      StatusUnauthorized|StatusForbidden, gin.H{"error": <code>})` in the
      six CRUD handlers, `recipe_requests.go`, and `validation.go` is
      replaced by the matching helper call. `grep -rn 'gin.H{"error"'
      internal/handler/*.go` (non-test) returns only `errors.go`'s five
      helpers + `internalError` + the `auth_handler.go` exceptions
      (`429` bodies).
- [ ] `auth_handler.go`: all generic-shape `badRequest` / `notFound` /
      `conflict` / `unauthorized` / `forbidden` sites on the helpers; all
      `StatusInternalServerError` sites that have a preceding
      `c.Error(fmt.Errorf(...))` on `internalError` with an unchanged log
      string; all matching `ShouldBindJSON` triplets on `bindJSON`.
- [ ] `auth_handler.go`: `GoogleCallback` redirect errors and the three
      `429` / cooldown bodies are **unchanged**.
- [ ] `middleware/rate_limit.go`: body is `{"error":"rate_limit_exceeded"}`
      on limit, `{"error":"server_error"}` on limiter failure;
      `rate_limit_test.go:79,118` assertions updated to match, suite green.
- [ ] `go test ./internal/handler/...` green — no assertion changes in
      any handler test (behaviour preserved).
- [ ] `go test ./internal/middleware/...` green.
- [ ] `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0
      ./...` clean; `gofmt` clean; `go mod tidy -diff` clean (no new
      deps).

## Verification

| Part | Mode | Command / artifact |
| --- | --- | --- |
| Handler-package retrofit (6 CRUD + `validation.go` + `auth_handler.go`) | Refactor under "logic with assertable behaviour", behaviour-preserving — **no new test**; guard is the existing suite, which already asserts `w.Code` **and** `body["error"]` for every error path | `go test ./internal/handler/...` — must stay green with zero test edits |
| `middleware/rate_limit.go` (finding 6) | Intentional body change — update the two existing assertions, confirm green | `go test ./internal/middleware/...` |
| Lint / format | Gate | `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`; `gofmt`; `go mod tidy -diff` |
| Review | Gate — `auth_handler.go`'s ~60-site piece gets the attention | `/code-review medium main` |

No visual / screenshot mode (backend only). No interactive mode —
nothing user-facing changes; the `rate_limit.go` code change is covered
by the middleware test.

## Piece order (AI-driven — refactor → run suite → confirm green → stop)

1. **`handler/errors.go`** (`badRequest`/`notFound`/`conflict`) + the four
   reference-data CRUD handlers (`item_category`, `item`, `unit`,
   `recipe_category`). **Done** — 28 sites, `go test ./internal/handler/...`
   green, zero test edits.
2. **`recipe_handler.go` + `recipe_requests.go` + `validation.go`** (+
   `unauthorized` helper). **Done** — 37 sites, `net/http` dropped from
   `recipe_requests.go` / `validation.go`, suite green, zero test edits.
3. **`auth_handler.go`** full retrofit (+ `forbidden` and `serverError`
   helpers; `internalError` now delegates to `serverError`). **Done** —
   54 error sites + 6 `bindJSON`, suite green, zero test edits.
4. **`middleware/rate_limit.go`** (`rate_limit_exceeded` / `server_error`)
   + the two `rate_limit_test.go` assertions. **Done** — middleware
   suite green.

Gate: `golangci-lint` 0 issues, `gofmt` clean, `go vet` clean,
`go mod tidy -diff` clean, full non-DB `go test ./...` green.

Next: `/code-review medium main` → `/close-out`.

Grilled 2026-08-31.

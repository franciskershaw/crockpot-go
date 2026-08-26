# CROC-009a — CORS middleware

Grilled 2026-08-26 (as part of `crockpot-react` CFE-002's grill).
Completed 2026-08-26.

**Implementation mode: hand-written (founder).** Small, mechanical port
with a direct precedent. `grill-me` produced this doc; Claude writes no
code. Test-first (the precedent tests are deterministic). After
implementation: `/code-review` is available (ask first per `CLAUDE.md`),
or CodeRabbit via PR — founder's call on session budget.

## Context

`CROC-001` (`docs/handoffs/CROC-001.md:234,256`) deferred CORS to "later
tickets"; `CROC-005` built only rate-limiting; nothing since picked it
up. Confirmed 2026-08-26: `internal/middleware/` contains only `auth.go`
and `rate_limit.go`, and `grep -rni "access-control\|cors"` over all
non-test `.go` files returns nothing.

`crockpot-react`'s auth flow (CFE-002/002a) makes cross-origin XHR calls
— `POST /auth/refresh` and `GET /me` from `http://localhost:5173` to
`http://localhost:8080`, both with `credentials: "include"`. Without CORS
response headers the browser blocks them, and the log-in milestone is
impossible. The Google redirect flow itself (`/auth/google/login`,
`/auth/google/callback`) is unaffected — those are top-level browser
navigations, not XHR — so this is specifically about the two JSON
endpoints the SPA calls with `fetch`.

## Decisions and why

- **Straight port of `packing-list-go/internal/middleware/cors.go`** (29
  lines) and its `cors_test.go` (85 lines, 3 tests). Checked current
  source 2026-08-26. `func CORS(allowedOrigin string) gin.HandlerFunc`:
  echoes `Access-Control-Allow-Origin` only when the request `Origin`
  header exactly equals `allowedOrigin`; sets
  `Access-Control-Allow-Credentials: true`; on `OPTIONS`, adds
  `Allow-Methods` / `Allow-Headers` and `c.AbortWithStatus(204)`;
  otherwise `c.Next()`. No behavioural change from the precedent.
- **Single origin, `cfg.FrontendURL`**, not a list — decided at the
  grill. `packing-list-go` wires exactly `middleware.CORS(cfg.FrontendURL)`;
  `cfg.FrontendURL` is already loaded and validated-required in
  `config/config.go` (`= http://localhost:5173` in `.env`). No deployed
  `crockpot-react` this round (Vercel deferred), and no deployed
  `crockpot-go` at all yet, so a multi-origin `CORS_ALLOWED_ORIGINS`
  list would be building for a deployment whose shape (custom domain?
  `crockpot.app`?) isn't decided. The API-deploy ticket revisits CORS
  for the production origin regardless.
- **Register globally, in the top-level middleware chain**, so a
  preflight `OPTIONS` is answered and aborted before any route-group
  rate limiter (`authTight` 10/min, `authRefresh` 30/min) runs — Gin
  runs `server.Use()` middleware before group `.Use()` middleware.
  Consider placing it **before** the global rate-limit `server.Use()`
  too, so preflights don't spend the 120/min global bucket; this is a
  minor divergence from `packing-list-go`'s order (which has CORS after
  rate-limit) and is a judgement call, not a requirement — note the
  choice either way.
- **`allowedOrigin == ""` guard.** `packing-list-go`'s version compares
  `origin != allowedOrigin` directly; if `allowedOrigin` were ever `""`
  a request with no `Origin` header would spuriously match. `cfg.FrontendURL`
  is validated non-empty at load (`config/config.go` required list), so
  this can't happen here — but the port should keep
  `packing-list-go`'s exact logic rather than "improve" it; the config
  validation is the real guard and duplicating it in the middleware is
  noise.

## Acceptance criteria

- [ ] `internal/middleware/cors.go`: `func CORS(allowedOrigin string)
      gin.HandlerFunc`, byte-for-byte behaviour of
      `packing-list-go/internal/middleware/cors.go` (import path aside).
- [ ] `internal/middleware/cors_test.go` (`package middleware_test`):
      `TestCORS_AllowsConfiguredOrigin`,
      `TestCORS_OmitsHeadersForUnconfiguredOrigin`,
      `TestCORS_HandlesPreflightWithoutCallingHandler` — ported from the
      precedent, `http://localhost:5173` as the fixture origin.
- [ ] `main.go`: `server.Use(middleware.CORS(cfg.FrontendURL))` added to
      the global chain (see ordering note above). No route-group
      changes.
- [ ] `go test ./internal/middleware/...` passes; `go build ./...`;
      `gofmt -l .` clean; `golangci-lint run --max-same-issues=0
      --max-issues-per-linter=0 ./...` clean; `go mod tidy -diff` (no
      new dependency — `gin` only).
- [ ] Real cross-origin check as part of CFE-002a's interactive
      verification (that ticket isn't grilled yet — see `crockpot-react`
      master-spec "Round 1"): with `crockpot-go` running locally and
      `crockpot-react` on `:5173`, the browser completes
      `POST /auth/refresh` and `GET /me` with no CORS console error, and
      a request from a non-configured origin gets no
      `Access-Control-Allow-Origin` header.

## Non-goals

- No multi-origin list, no `Access-Control-Max-Age`, no configurable
  methods/headers beyond the ported set.
- No production-origin wiring — that rides the future API-deploy ticket.
- **Not addressed here (observed during the grill, worth their own
  ticket or a tech-debt pass):** `crockpot-go` also has no `BodyLimit`
  or `ErrorLogger` middleware, both of which `packing-list-go` has and
  the master-spec's non-functional section assumes (the "1 MB JSON body
  cap"). `_ = c.Error(...)` calls throughout `auth_handler.go` currently
  rely on Gin's default logger, not a dedicated `ErrorLogger`.

## Verification modes

- **Logic with assertable behaviour** — the 3 ported tests, failing
  first (stub `CORS` returning a no-op `gin.HandlerFunc` so each test
  fails on a missing header / wrong status, not a panic), then the real
  body. `go test ./internal/middleware/...`.
- **Service/API boundary** — folded into CFE-002a's interactive Google
  round-trip; this is CORS's first real proof against a browser.

## Roadmap

1. **Write `internal/middleware/cors_test.go` first** (`package
   middleware_test`), copying the 3 tests from
   `packing-list-go/internal/middleware/cors_test.go:1-85`. Add a stub
   `func CORS(allowedOrigin string) gin.HandlerFunc { return func(c
   *gin.Context) { c.Next() } }` in `cors.go`. `go test
   ./internal/middleware/...` → confirm all 3 red (missing headers /
   200 instead of 204), no panic.
2. **Implement `cors.go`** from `packing-list-go/internal/middleware/cors.go:1-30`,
   changing only the package doc if desired. `go test
   ./internal/middleware/...` → green.
3. **Wire in `main.go`**: add `server.Use(middleware.CORS(cfg.FrontendURL))`.
   Precedent line: `packing-list-go/main.go:86`. Decide ordering vs. the
   global rate-limit `server.Use()` (line ~82 in current `main.go`) —
   before it, per the decision above, unless you prefer matching
   `packing-list-go`.
4. **Full check**: `go build ./...`, `gofmt -l .`, `go vet ./...`,
   `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0
   ./...`, `go mod tidy -diff`.
5. Open a PR (CodeRabbit), or ask about `/code-review` — per `CLAUDE.md`,
   don't auto-run it.

## Close-out

Implemented AI-driven (flipped from hand-written on request) — TDD
red→green clean, no rework. Wired in `main.go` **before** the global
rate limiter so preflight `OPTIONS` don't spend the bucket. CodeRabbit
raised one finding, `Vary: Origin` (cache correctness — preventive, not
a live bug given single-origin + credentialed non-cacheable endpoints);
added it test-first. Also added `.coderabbit.yaml` with a
`docs/handoffs/**` path instruction so handoff status lines stop drawing
review findings. `go test ./internal/middleware/...`, `go build`,
`gofmt`, `go vet`, `golangci-lint` (0 issues), `go mod tidy -diff` all
clean; PR #9 CI green. Browser cross-origin proof still owed by CFE-002a
per the verification plan above. Completed 2026-08-26.

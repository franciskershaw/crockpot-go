# CROC-001 — Project scaffold, config, DB connection, sqlc & migrations

**Implementation mode: hand-written.** This ticket is being implemented
by the founder directly, referencing `packing-list-go`'s actual files
(`config/config.go`, `db/db.go`, `main.go`, `lifecycle.go`) plus this doc
— not by Claude, and not test-first. See `CLAUDE.md`'s "Overrides of the
global default process" for what this mode does and doesn't skip. Claude
runs a `code-review` pass on the diff once it's written, and checks the
verification evidence below, before this closes out.

## Context

Bootstrap the Go service: load configuration from the environment,
connect to Postgres (Neon) via pgx/sqlc, wire `golang-migrate`, and stand
up a Gin server with a public health check, so later tickets have
somewhere to hang routes and a repository layer to build on. Mirrors
`packing-list-go`'s `PACK-001` in shape, but this project uses sqlc on
top of pgx instead of hand-scanned pgx — new ground, not a straight copy
(`docs/specs/master-spec.md`, "Key architecture decisions").

## Decisions and why

- **Config loads everything now, not just what CROC-001 uses.** Mirrors
  `packing-list-go/config/config.go:12-25` field-for-field: `Port`,
  `Environment`, `DatabaseURL`, `JWTSecretAccess`, `JWTSecretRefresh`,
  `JWTSecretOAuthState`, `GoogleClientID/Secret/RedirectURL`,
  `FrontendURL`, `TrustedProxies`. Required-at-load-time (fail fast, same
  set `packing-list-go` requires): `DatabaseURL`, `JWTSecretAccess`,
  `JWTSecretRefresh`, `JWTSecretOAuthState`, `FrontendURL`. Google OAuth
  fields load but aren't validated as required (matches
  `packing-list-go`'s own asymmetry — those become load-bearing only once
  CROC-004 exists). Chosen over trimming to DB+port now because Go's
  compiler forces every call site of a changed function signature to
  update in lockstep — `packing-list-go`'s `PACK-015` ("config threading
  shipped; large ripple", 15 files touched) is a real instance of that
  pain, even though that ripple was from a different kind of signature
  change, not literally late-added env vars.
- **Neon pooled-endpoint connection mode is fixed from day one, not
  deferred.** `packing-list-go/db/db.go:27-36` documents that Neon's
  pooled endpoint runs PgBouncer in transaction-pooling mode, which is
  incompatible with server-side prepared statements under concurrent
  queries (interleaved Parse/Bind across requests sharing a backend
  connection) — and works around it by forcing
  `pgx.QueryExecModeSimpleProtocol`. This project's `sqlc` config uses
  `sql_package: "pgx/v5"` (native pgx, not `database/sql`+stdlib, per
  master-spec's "sqlc on top of pgx"), so the equivalent fix is set on
  `pgxpool.Config.ConnConfig.DefaultQueryExecMode` before the pool is
  created. Decided now rather than "default and revisit" because every
  sqlc-generated repository in later epics (15+ tables) is built on top
  of whatever pool config CROC-001 establishes — retrofitting after the
  fact means touching every repository, not just `db/db.go`.
- **sqlc scope is config-only for this ticket.** CROC-001 runs before
  CROC-002 creates any schema, so there's nothing real for `sqlc
  generate` to produce yet. AC is: `sqlc` installed, `sqlc.yaml` written
  (schema: `db/migrations`, queries: `internal/sqlc/queries`, output:
  `internal/sqlc`, per the folder layout `CLAUDE.md` already documents),
  and `sqlc generate` exits 0 against whatever's there. The first real
  proof that the pipeline produces usable Go happens naturally in
  CROC-002 once the schema lands — no placeholder table/query built here
  just to prove the pipe works end to end.
- **Server bootstrap hardening included now, not deferred.**
  `docs/specs/master-spec.md`'s non-functional section already commits to
  specific timeout values (`ReadHeaderTimeout` 5s, `ReadTimeout` 10s,
  `WriteTimeout` 15s, `IdleTimeout` 60s) as day-one defaults, not
  something to tune later — so `main.go` builds a custom `http.Server`
  (mirrors `packing-list-go/lifecycle.go:26-36`'s `newHTTPServer`) and
  handles `SIGTERM`/`os.Interrupt` via `signal.NotifyContext` +
  `httpServer.Shutdown(ctx)` with a grace period, mirroring
  `packing-list-go/main.go:158-182` minus the parts that don't exist yet
  here (token sweeper, auth routes, rate-limit middleware — those are
  later epics, not CROC-001).
- **Boot failures exit non-zero with a logged error, never panic or fail
  silently.** Config load, DB init, and `httpServer.ListenAndServe`
  failures all `os.Exit(1)` after printing the error — matches
  `packing-list-go/main.go:41-52,166-171` and `PACK-001`'s own AC.

## Constraint found during grill-me (verified, not guessed)

`go:embed` on an empty directory — or one containing only a
dot-prefixed file — fails to compile: `pattern migrations: cannot embed
directory migrations: contains no embeddable files`. Confirmed by
building a throwaway module and testing all three cases (empty dir,
`.gitkeep` only, one real file) — only the real-file case builds.

Since `db/migrations/` has no real schema until CROC-002, `db/db.go`'s
`//go:embed migrations` would fail to compile as part of this ticket
unless the directory has at least one non-hidden file. **Resolution:**
CROC-001 creates a placeholder `db/migrations/000001_init.up.sql` /
`.down.sql` pair (comment-only, e.g. `-- placeholder, real schema lands
in CROC-002`), just enough to satisfy `go:embed` and let `migrate up`
run as a no-op. CROC-002 **replaces the contents of this same numbered
migration** with the real schema rather than adding `000002` — these are
pre-launch, never-applied-to-a-real-environment migrations, so editing
000001 in place is fine and avoids a permanently pointless empty
migration sitting in the repo. Noting this on CROC-002's backlog entry
too so it isn't rediscovered as a surprise.

## Acceptance criteria

- [ ] `go run .` boots successfully against a local `DATABASE_URL`
      pointed at the real Neon dev DB (never Docker/local Postgres, same
      rule as this project's repository-layer tests).
- [ ] Config load fails fast with a clear error if any required env var
      (`DATABASE_URL`, `JWT_SECRET_ACCESS`, `JWT_SECRET_REFRESH`,
      `JWT_SECRET_OAUTH_STATE`, `FRONTEND_URL`) is missing.
- [ ] `db.InitDB()` opens a `pgxpool.Pool` with
      `DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol` set, pings
      it, and runs pending migrations from the embedded
      `db/migrations` FS; `db.CloseDB()` releases it on shutdown.
- [ ] `migrate up` / `migrate down` both run clean (no-op against the
      placeholder migration).
- [ ] `sqlc.yaml` is valid and `sqlc generate` exits 0.
- [ ] `GET /health` returns 200 with a welcome message, unauthenticated,
      no DB round-trip on the request path (DB health is proven once at
      boot via the `Ping()` in `InitDB`).
- [ ] Server exits non-zero with a logged error (not a panic, not
      silent) if config load, DB init, or `httpServer.ListenAndServe`
      fails.
- [ ] `SIGTERM`/`Ctrl-C` triggers graceful shutdown: in-flight requests
      finish (or the grace period expires), then the process exits 0.
- [ ] `.env.example` lists every var `config.Load()` reads.

## Non-goals

- No auth logic, no business-domain tables, no handlers beyond `/health`
  — this ticket only stands up the scaffold.
- No rate limiting, CORS, or body-size middleware — later tickets per
  the master spec's non-functional section.
- No real sqlc queries or generated repositories — CROC-002 onward.

## Verification modes

- **Service/API boundary** (`~/.claude/CLAUDE.md`): a real request
  against the real dependency. Concretely: run `go run .` against the
  real Neon dev `DATABASE_URL`, then `curl localhost:<PORT>/health` and
  confirm `200` + the welcome JSON. Then send `SIGTERM` to the running
  process and confirm it logs a shutdown message and exits 0.
- **Anything interactive**: same action as above, done by hand by the
  founder — this is also the "hands-on use" check the global process
  asks for; one action satisfies both modes here.
- **Limits/config**: the four `http.Server` timeout values and the
  `pgxpool` simple-protocol setting aren't independently exercised by a
  synthetic test in this ticket — they're inherited, known-good values
  from `packing-list-go`'s own production use, not new numbers being
  tuned here.
- **Code review**: once hand-written, Claude runs `/code-review` on the
  diff before close-out (this ticket's implementation-mode override, see
  `CLAUDE.md`).
- **Lint**: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
  — run for real, not skipped; works with no `.golangci.yml` present
  (built-in defaults).
- **Tests**: none for this layer, matching `packing-list-go`'s own
  `PACK-001` precedent (infra/bootstrap code exempted from tests-first —
  see its handoff doc). Explicit exemption, not an oversight.

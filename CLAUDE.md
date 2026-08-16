# Crockpot API

Follows the global development process — see `~/.claude/CLAUDE.md`.

## Reference projects

- **Functional reference**: `../../crockpot` (Next.js/Prisma/MongoDB, the
  app this API replaces) — check `prisma/schema.prisma` and `src/` before
  assuming a feature's exact behaviour rather than guessing from the name.
  Also the source data for `cmd/migrate-data` (Epic 8).
- **Design reference**: `../screenshots/` (not committed) — Claude-Design
  reskins covering landing page, browse, recipe detail, add-recipe, and
  the "your crockpot" tabs (menu/planner/favourites/my-recipes). Not
  pixel-perfect or final UX, but the source for the weekly-planner
  (Epic 9) and paste-a-link recipe import (Epic 10) feature shapes — check
  the actual PNGs before assuming a flow, same rule as any other claim.
- **Architectural reference**: `../../packing-list/packing-list-go` — prior
  Go/Postgres project by the same author. Its `CLAUDE.md`, `LESSONS.md`,
  and `docs/specs/master-spec.md` are the source for the patterns this
  project deliberately reuses (see below). Its CI workflow
  (`.github/workflows/ci.yml`) is the template for this project's deploy
  pipeline when that ticket comes up — reuses the same Neon + Docker +
  nginx + DigitalOcean droplet pattern; pick an unused port (droplet
  already runs `packing-list-api` on 5400, `events-api` on 5500,
  `salary-split-api` on 5300).

## Stack

- Go, Gin, PostgreSQL (Neon)
- DB access: **sqlc** on top of pgx (not raw hand-scanned pgx like
  `packing-list-go` — decided explicitly at kickoff because Crockpot's
  schema is roughly double the table count)
- Migrations: `golang-migrate`, plain up/down SQL files in
  `db/migrations/`
- Auth: Google OAuth2/OIDC **and** email/password (with email confirmation
  + forgot-password), both issuing the same JWT access (15 min) / refresh
  (7-day sliding, httponly cookie, rotating, DB-backed) token pair as
  `packing-list-go`
- Rate limiting: `ulule/limiter`
- Email: Resend (verification + password-reset emails)
- Images: Cloudinary, uploaded client-side by the frontend — the API only
  ever stores the resulting URL, never proxies image bytes
- Testing: stdlib `testing` + `testify/mock` for handler-layer repository
  mocks (same override as `packing-list-go` — see its `CLAUDE.md`)
- Deployment: Docker (multi-stage to distroless), behind nginx, on the
  same DigitalOcean droplet as `packing-list-go`

## Architecture

- **Repository interfaces are defined in the `handler` package**
  (consumer-defined), not in `repository`. Each `Postgres*Repository` under
  `internal/repository` implements the interface(s) its handlers need,
  built on sqlc-generated queries.
- **Handlers are structs** with injected dependencies, not standalone
  functions.
- **Ownership model**: reference data (item categories, items, units,
  recipe categories) is system-owned and admin-managed only — no
  per-user copies (this differs from `packing-list-go`, where users could
  extend categories/items with their own). Recipes carry `created_by_id`
  + `approved`: creators see their own unapproved recipes, everyone sees
  approved ones, only ADMIN can approve, ADMIN's own recipes are
  auto-approved.
- **No soft deletes** currently planned for any entity (unlike
  `packing-list-go`'s archived packing lists) — add one only when a
  specific ticket needs it.
- **Tiers**: FREE (core product, ≤5 own recipes), PREMIUM (unlimited
  recipes + weekly planner + link import), PRO (reserved enum value,
  nothing built against it yet). Granted manually by an admin until
  billing (Epic 11) exists — no separate beta-access flag.
- Full rationale for every decision above: `docs/specs/master-spec.md`.

## Overrides of the global default process

- **Hand-written tickets.** On a per-ticket basis (not a default), the
  founder may choose to implement a ticket by hand instead of Claude
  doing it test-first — typically to build hands-on familiarity with a
  new part of the stack (e.g. CROC-001's Go server/DB/sqlc scaffold).
  When this is chosen: `grill-me` still runs and still produces the
  normal handoff doc, but Claude writes no code for that ticket — not
  even TDD stubs. The founder implements against the handoff doc plus
  whatever reference files it cites. Once done, Claude runs a
  `code-review` pass on the diff and confirms the ticket's verification
  mode was actually exercised (real command run, real screen looked at)
  before close-out — same gate as a Claude-implemented ticket, just
  applied to code Claude didn't write. `grill-me` asks which mode
  applies at the start of each ticket rather than assuming either way.
  Hand-written handoff docs also get a **Roadmap** section below the
  acceptance criteria: an ordered, numbered build sequence (dependency
  install → file-by-file creation → manual verification), each step
  citing the exact reference file/lines to work from and calling out
  deltas where this project's code deliberately diverges from its
  reference. For a ticket whose verification mode is normally
  tests-first, the roadmap inserts a "write failing test stubs, confirm
  red" step before each piece's "make it pass" step, per
  `packing-list-go/CLAUDE.md`'s "TDD in Go" shape — CROC-001 itself
  skipped that step because this layer is test-exempt, not because the
  roadmap format omits it by default.
- **AI-driven tickets.** Claude works one coherent piece at a time (roughly
  file-sized, matching hand-written mode's roadmap granularity above):
  write the failing test(s), write a minimal stub that fails for the
  right reason (never a panic), confirm red, then **stop and report the
  diff for review** — no real implementation until the founder gives the
  go-ahead. On go-ahead: implement, confirm green, then stop again before
  starting the next piece. Applies regardless of Auto Mode, which governs
  whether Claude pauses to ask clarifying questions about ambiguous
  requirements, not whether it pauses at these checkpoints. A test
  confirmed red for the right reason is locked once that stop happens —
  Claude implements to satisfy it, it does not edit the test's assertions
  to fit whatever gets implemented afterward. If a locked test later turns
  out to be wrong, that discovery is itself a stop-and-flag moment, not a
  silent edit.

## Folder layout (once CROC-001 scaffolds it)

Mirrors `packing-list-go`:

```
config/           Environment/config loading
db/               DB connection setup, migrations, seed SQL
internal/
  auth/           Google OAuth manager, JWT issuing/verification, bcrypt
  handler/        HTTP handlers + the repository interfaces they consume
  middleware/     Auth, CORS, rate limiting, body-size limits, error logging
  models/         Domain types shared across handler/repository
  repository/     Postgres implementations of the handler-defined interfaces
  sqlc/           sqlc-generated code (queries + generated structs)
  testutil/       Shared test helpers
docs/
  specs/          Master spec + ticket backlog
  handoffs/       Per-ticket handoff docs written before implementation
```

## Verification commands

- Handler tests (no DB): `go test ./internal/handler/...`
- Repository tests (integration, real Neon dev DB — never Docker/local
  Postgres): `DATABASE_URL=$DATABASE_URL go test ./internal/repository/...`
- Lint (uncapped, matches CI):
  `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
- Format: `gofmt` — canonical, no formatter choice to make.
- `go mod tidy`: deferred to ticket-end, not run per piece — mid-ticket it
  prunes any dependency added ahead of the file that imports it, causing
  churn. CI only ever checks `go mod tidy -diff` (read-only, fails if it
  would change anything), never rewrites `go.mod`/`go.sum` itself. Adding
  a dependency mid-ticket: use `go get <pkg>@<version>` alone, which
  updates precisely that dependency without a full-tree prune.

## Docs

- `docs/specs/master-spec.md` — living spec + ticket backlog
- `docs/handoffs/CROC-NNN.md` — one per ticket
- `LESSONS.md` — retro log, reviewed each kickoff/grill-me

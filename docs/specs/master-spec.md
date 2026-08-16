# Crockpot API — Master Spec

## Goals

A recipe management and meal-planning API: browse/search recipes, plan a
weekly menu, and auto-generate shopping lists from that menu. This is a
from-scratch rebuild of the [`crockpot`](https://github.com/franciskershaw/crockpot)
Next.js full-stack app as a separate Go API + frontend, to fix performance
issues in the combined app. The Next.js app (and its `prisma/schema.prisma`)
is the functional reference for ~90% of this API's scope — not a design
document, real running code to check claims against.

Primary users today are the founder and their partner, but the system is
built to serve many independent users, with a tiered role model (free vs.
paid) and an eventual path to paid subscriptions.

## Core use cases

- Sign in with Google OAuth, or register with email/password (email
  confirmation required before login; forgot-password flow to reset).
- Browse and search public (approved) recipes with instructions,
  ingredients, and images.
- Favourite recipes for quick access.
- Submit a custom recipe; it stays private to its creator until an admin
  approves it for public listing. The admin account (founder) can create
  recipes that are public immediately, no approval step.
- Build a weekly menu by adding recipes (with a serving size per entry).
- Auto-generate a shopping list from the current menu, aggregated by
  ingredient; manually add extra items; mark items obtained.
- Reference data (item categories, items, units, recipe categories) is
  system-owned and shared across all users; only admins manage it.

## Tiers

Resolved via kickoff discussion — the goal was to keep the app's original
value proposition (killing the day-to-day shopping list chore) free
forever, and only charge for genuine time-saving extras:

- **FREE**: browse/search approved recipes, favourite, flat weekly menu,
  auto-generated shopping list, up to **5** own custom recipes (pending
  admin approval as usual).
- **PREMIUM**: everything in FREE, plus unlimited custom recipes, the
  **weekly planner** (day × meal-slot calendar, Epic 9), and **recipe
  import from a pasted link** (Epic 10).
- **PRO**: kept as a reserved enum value only. Not built, not marketed,
  not gated against anything in v1 — a third tier only earns its
  complexity once a feature has genuinely different, usage-based cost
  (e.g. a real AI feature with per-call marginal cost). Revisit if/when
  such a feature is actually designed.
- **ADMIN**: unchanged — full access, recipe approval, own recipes
  auto-approved, unrestricted count.

Until billing (Epic 11) exists, PREMIUM is granted the same way the old
app granted ADMIN: manually, by an admin. No separate beta-access flag.

## Non-goals (current scope)

- No billing/subscription **enforcement in the initial build** — Stripe
  integration itself is scheduled (Epic 11, not just designed-and-shelved)
  but is not a blocker for shipping the core product or the PREMIUM
  features above; PREMIUM access is manually granted until it lands.
- No full admin panel (user management, usage stats, category/unit CRUD
  UI). The only admin-gated behaviour in v1 is recipe approval and
  unrestricted recipe creation.
- No sharing/collaboration between users on menus, planner, or shopping
  lists.

## Key architecture decisions

- **Stack**: Go, Gin, PostgreSQL (Neon) — same stack as the founder's prior
  Go project ([`packing-list-go`](../../../../packing-list/packing-list-go)),
  reused deliberately for consistency: Gin, `golang-migrate` with plain
  up/down SQL files, stdlib `testing` + `testify/mock` for handler tests,
  `ulule/limiter` for rate limiting, Docker multi-stage build behind nginx,
  deployed to the same Neon + Docker/nginx pattern.
- **DB access: sqlc on top of pgx**, not raw hand-scanned pgx like
  `packing-list-go`. Decided explicitly during kickoff: Crockpot's schema
  (recipes/ingredients/menus/shopping-lists/categories/units, ~15 tables)
  is large enough that generated scan code earns its keep over
  `packing-list-go`'s 6-7-table hand-written pattern. Repository pattern is
  otherwise identical — interfaces defined in `handler`, implemented in
  `repository`, sqlc generates the query/scan layer those implementations
  call into. `sql_package: "pgx/v5"` (native pgx, not `database/sql`), a
  `*pgxpool.Pool` as the `DBTX`. Fixed at CROC-001, before any repository
  exists: `pgxpool.Config.ConnConfig.DefaultQueryExecMode =
  pgx.QueryExecModeSimpleProtocol`, because Neon's pooled endpoint runs
  PgBouncer in transaction-pooling mode, incompatible with server-side
  prepared statements under concurrent queries — the same interaction
  `packing-list-go/db/db.go:27-36` already diagnosed and worked around.
  Decided once, up front, so it doesn't need retrofitting across 15+
  tables' worth of generated repositories later. Revisit only if Crockpot
  moves off Neon's pooled endpoint (e.g. onto a direct/unpooled
  connection string), which would remove the reason for the workaround.
- **Relational schema replaces Mongo's embedded documents.** Prisma/Mongo
  used embedded arrays/subdocuments (`Recipe.ingredients`,
  `RecipeMenu.entries`, `ShoppingList.items`, `*Ids` array fields for
  many-to-many). Postgres uses real join tables and FK'd child tables
  instead:
  - `recipe_ingredients` (recipe_id, item_id, unit_id nullable, quantity)
    replaces embedded `RecipeIngredient[]`
  - `recipe_categories_recipes`, `item_allowed_units`,
    `recipe_favourites` replace the `*Ids String[]` many-to-many arrays
  - `recipe_menu_entries`, `menu_history_entries` replace `RecipeMenu`'s
    embedded `entries`/`history`
  - `shopping_list_items` replaces `ShoppingList`'s embedded `items`
  - One `recipe_menu` and one `shopping_list` row per user (matching the
    Mongo model's one-per-user constraint via a unique `user_id`)
- **Data migration**: a one-off Go CLI (`cmd/migrate-data`) inside this
  repo, not a throwaway external script — reads every collection from the
  existing MongoDB instance and writes into the new Postgres schema via
  the same repository layer the running API uses, so the same
  create/validation logic applies to migrated rows as to rows created
  normally. Rerunnable against the dev database (safe to wipe-and-reload)
  so the founder always has real recipe data to build the frontend
  against; run once for real at prod cutover. See Epic 8.
- **Auth**: Google OAuth2/OIDC *and* email/password, both issuing the same
  JWT pair as `packing-list-go` — 15 min access token (bearer header,
  stateless), 7-day sliding refresh token (httponly cookie, DB-backed,
  rotates on every use, reuse revokes the whole family). Password accounts
  require email confirmation before first login — a 6-digit OTP code (not
  a link; decided at `CROC-005`, `docs/handoffs/CROC-005.md`, after the
  founder flagged that a clickable confirmation link reads as
  spam/phishing in the wrong context), 10-minute TTL, invalidated after 5
  wrong attempts; forgot-password issues a separate, single-use, expiring
  reset token (shape TBD at `CROC-007`'s own grill — not assumed to be the
  same OTP pattern). Password hashing via `golang.org/x/crypto/bcrypt`
  (max 72 bytes, rejected explicitly rather than silently truncated).
  **Correction**: the original claim here that this "match[es] the old
  Next.js app's `bcryptjs` choice" was checked and found false at
  `CROC-005` — the old app has no password field or flow at all
  (`bcryptjs` was an unused dependency, never imported); email/password
  auth has no functional precedent in either reference project and was
  designed first-principles.
  **Google and password login are mutually exclusive per user, not
  linkable** — decided at `CROC-002` (`docs/handoffs/CROC-002.md`):
  whichever method created the account is the account, no `accounts`
  join table, `google_id`/`password_hash` live directly on `users`.
  Rejected: Auth.js-style multi-provider linking (what the old Next.js
  app's `Account` model supports) — real complexity (merge/conflict
  rules, an extra table) for a convenience this app's scale doesn't need
  yet. Revisit only if a real product need for one person to use both
  methods on one account shows up.
  **Access-token claims embed `role`** — decided at `CROC-003`
  (`docs/handoffs/CROC-003.md`): `Epic 7`'s tier-gating middleware checks
  `claims.Role` directly, no DB round-trip on gated requests. Rejected:
  fetching `role` from the DB per gated request (always fresh, but a
  lookup on every tier-gated endpoint, and gating middleware would need
  a `UserRepository` it otherwise wouldn't). Accepted trade-off: a role
  change takes up to 15 minutes (one access-token cycle) to take effect,
  matching `PACK-027`'s "no compliance driver for this personal app"
  reasoning for a similar staleness call. Revisit only if a real
  instant-revocation requirement shows up — which would need a bigger
  fix (session invalidation) than switching this decision alone solves.
  **Google sign-in verifies an OIDC `id_token`** (`coreos/go-oidc`,
  signature checked locally against Google's cached JWKS), not a call to
  Google's userinfo endpoint — decided at `CROC-004`
  (`docs/handoffs/CROC-004.md`) after finding `packing-list-go`'s
  `cfg.GoogleOAuth2Config` (copied into this project at `CROC-001`) is
  dead code there, never consumed outside its own construction; the real
  precedent is `internal/auth/google.go`'s independently-built OIDC
  manager. **The Google callback issues and persists only the refresh
  token** — no access token is minted or returned at that step; the
  frontend mints its own via `POST /auth/refresh` on load, matching
  `crockpot-react`'s master-spec commitment. `RefreshTokenRepository` is
  one interface split across two tickets: `CROC-004` builds
  `CreateFamily`/`DeleteStaleFamiliesForUser` only; `CROC-008` adds
  `RotateFamily`/`FindFamilyByID`/`RevokeFamily` to the same
  interface/struct. **A Google sign-in against an email already
  registered via password fails explicitly** (`ErrEmailRegisteredWithPassword`,
  detected as a Postgres unique-violation on `users.email`), redirecting
  the browser to `${FRONTEND_URL}/auth/callback?error=...` rather than
  either merging accounts or surfacing a raw 500 — the direct, foreseeable
  consequence of the "mutually exclusive, no linking" decision above, not
  a hypothetical deferred until password auth ships.
- **Ownership model**: reference data (`item_categories`, `items`, `units`,
  `recipe_categories`) is system-level, visible to everyone, admin-managed
  only — no per-user copies (unlike `packing-list-go`, which let users
  extend categories/items; Crockpot's reference data is fully curated).
  Recipes have a `created_by_id` and an `approved` flag: creator can always
  see/edit their own unapproved recipes; everyone can see approved ones;
  only ADMIN can flip `approved`, and ADMIN-created recipes are
  auto-approved.
- **Repository pattern, handler structs, black-box handler tests**: same
  conventions as `packing-list-go` (interfaces owned by `handler`,
  `testify/mock` for repo mocks, `httptest` + `gin.New()`).
- **Images**: Cloudinary, uploaded directly from the frontend (client-side
  upload widget, matching the old app's `next-cloudinary` pattern) — the
  API stores the resulting URL/filename, never proxies image bytes itself.
  Keeps the 1 MB JSON body cap (below) meaningful.
- **Email**: Resend, for verification and password-reset emails (matching
  the old app's provider choice).
- **API error response shape**: locked in at `CROC-005` (previously
  implicit, consistent-by-coincidence across CROC-004's redirect-based
  errors and CROC-005's JSON ones, never actually decided). Applies to
  every future JSON-responding endpoint: failures return
  `{"error": "snake_case_code"}`, successes that aren't just the created/
  updated resource return `{"message": "human-readable text"}`.
  Validation failures (malformed/missing JSON fields) collapse to one
  generic `invalid_request` code, no field-level detail — deliberate:
  `crockpot-react`'s forms do their own client-side validation (required
  fields, email format) before ever submitting, so a real
  `invalid_request` from the API is an edge case (dev tools, a non-browser
  client), not a normal user-facing path worth building field-level
  precision for. Revisit if a real client ever needs to distinguish which
  field failed. `GoogleCallback`'s browser-redirect error shape
  (`?error=code`) stays the deliberate exception — it's a top-level
  navigation target, not a JSON API consumer.

## Non-functional expectations

**Load & latency**: no meaningful traffic yet (founder + partner, then
early users) — build for correctness over scale, but reuse
`packing-list-go`'s already-tuned HTTP server timeouts as sane defaults
rather than inventing new ones: `ReadHeaderTimeout` 5s, `ReadTimeout` 10s,
`WriteTimeout` 15s, `IdleTimeout` 60s. Revisit if/when paid signups make
this a real concern (tracked as a tech-debt-pass item, not a v1 ticket).

**Rate limiting & body caps**: reuse `packing-list-go`'s starting values —
global 120 req/min/IP, tighter limits on auth endpoints (login-type routes
10/min, refresh 30/min) — tuned per-route once real traffic patterns
exist. JSON body cap 1 MB (images never transit the API body). No ticket
prior to `CROC-005` owned actually building this — the middleware itself
(`ulule/limiter`, ported from `packing-list-go`) is first wired up there,
being the first ticket that needs it (see `docs/handoffs/CROC-005.md`).

**Session & revocation lifecycle**: access tokens 15 min, refresh tokens
7-day sliding expiry, rotate-on-use with reuse-detection revoking the
whole token family (same model as `packing-list-go`, see its
`docs/handoffs/PACK-027.md` for the reuse-detection design this copies).
Email-verification and password-reset tokens are single-use and
short-expiry (verification ~24h, reset ~1h — confirmed per-ticket, not
locked in at kickoff).

**Data retention**: no soft-delete requirement identified yet for any
Crockpot entity (unlike `packing-list-go`'s archived packing lists) —
revisit if/when a "trash" UX is wanted for recipes or menus.

## Ticket backlog

### Epic 1: Foundations
- **CROC-001** — Project scaffold. **Done.**
- **CROC-002** — Initial schema migration. **Done.**
- **CROC-003** — JWT helpers + auth middleware. **Done.**
- **CROC-003a** — CI checks pipeline + branch protection on `main`. **Done.**

### Epic 2: Authentication
*Sequencing corrected at `CROC-005`'s grill (`docs/handoffs/CROC-005.md`):
Epic 2 completes in full ticket order — CROC-004 (done) → 005 → 006 → 007
→ 008 → 009 — before any `crockpot-react` work starts. Supersedes the
plan noted at `CROC-004` (CROC-004 → CROC-008 → CFE-002 → CROC-005/006/007),
which was a miscommunication of "004 and 008 are next up" as "do
everything numbered between them later." The founder's actual bar for
starting frontend work is the whole auth epic being done, not a partial
session.*
- **CROC-004** — Google OAuth login flow. **Done.** See
  `docs/handoffs/CROC-004.md`.
- **CROC-005** — Email/password registration + confirmation. No
  functional precedent in either reference project — the old Next.js app
  has no password field/flow at all (Google + passwordless magic-link
  email only); designed first-principles, see `docs/handoffs/CROC-005.md`.
  `POST /auth/register` creates an unconfirmed user and sends a 6-digit
  OTP code via Resend (**not** a clickable link — reads as spam/phishing
  in the wrong context); `POST /auth/confirm` (`{email, code}`) verifies,
  10-minute TTL, invalidated after 5 wrong attempts; `POST
  /auth/resend-confirmation` (`{email}`) reissues, 60s per-email cooldown.
  Resending must delete/clear any existing unconsumed
  `email_verification_tokens` row for that user before inserting a new
  one — the partial unique index (`docs/handoffs/CROC-002.md`) enforces
  at most one active row but doesn't do this for you. Also the ticket that
  builds the base rate-limiting middleware (no earlier ticket owned it).
- **CROC-006** — Email/password login (`POST /auth/login`, rejects
  unverified accounts with a distinct error).
- **CROC-007** — Forgot/reset password (`POST /auth/forgot-password` sends
  reset email; `POST /auth/reset-password` consumes the token). Same
  clear-before-insert requirement as CROC-005, against
  `password_reset_tokens`.
- **CROC-008** — Refresh + logout (`POST /auth/refresh`, `POST
  /auth/logout`), rotation + reuse-detection per the design above.
- **CROC-009** — `GET /me` profile endpoint (id, email, name, image, role).
  First real protected route — also owns proving `CROC-003`'s
  `AuthMiddleware` works end-to-end against a live request (that ticket
  added no protected route of its own to test it against — see
  `docs/handoffs/CROC-003.md`).

### Epic 3: Reference Data
- **CROC-010** — Item categories CRUD (admin-only writes, public reads).
- **CROC-011** — Units CRUD (admin-only writes, public reads).
- **CROC-012** — Items CRUD, with allowed-units association (admin-only
  writes, public reads).
- **CROC-013** — Recipe categories CRUD (admin-only writes, public reads).

### Epic 4: Recipes
- **CROC-014** — Recipe creation (name, time, serves, instructions, notes,
  image URL/filename, ingredients, categories). Non-admin creators start
  `approved = false`; admin-created recipes are `approved = true`
  immediately. FREE-tier users are capped at 5 own recipes (409 once
  exceeded); PREMIUM/ADMIN unlimited.
- **CROC-015** — Recipe browse/search (approved recipes to everyone, plus
  the caller's own unapproved ones; filter by category/search term).
- **CROC-016** — Recipe update/delete (owner or admin only).
- **CROC-017** — Admin approval (`PATCH /recipes/:id/approve`, admin-only).
- **CROC-018** — Favourites (`POST`/`DELETE /recipes/:id/favourite`,
  `GET /recipes/favourites`).

### Epic 5: Meal Planning
- **CROC-019** — Menu read/upsert-entry (`GET /menu`, `POST /menu/entries`,
  `PATCH`/`DELETE /menu/entries/:recipeId`) — one menu per user, entries
  keyed by recipe with a serving count.
- **CROC-020** — Menu history tracking (increment/first/last-added,
  last-removed) as entries are added/removed — powers "you've made this
  before" style features later.

### Epic 6: Shopping Lists
- **CROC-021** — Generate/regenerate shopping list from current menu,
  aggregating quantities per ingredient.
- **CROC-022** — Manual item add/remove, obtained toggle
  (`PATCH /shopping-list/items/:id`), bulk mark-obtained.

### Epic 7: Roles & Tier Gating
- **CROC-023** — Role field on user (`FREE`/`PREMIUM`/`PRO`/`ADMIN`,
  default `FREE`), plus a reusable tier-gate helper (mirrors
  `packing-list-go`'s `internal/handler/ownership.go` pattern) used by
  Epic 4's recipe cap/approval, and later by Epics 9-10's PREMIUM gates.
  `PRO` is a valid enum value but nothing checks for it yet.

### Epic 8: Data Migration
- **CROC-024** — `cmd/migrate-data`: reads every collection from the
  existing MongoDB instance and writes into Postgres via the repository
  layer (see "Data migration" under Key architecture decisions). *AC: row
  counts per entity match between source and destination; safe to rerun
  against a wiped dev database; a real cutover run against prod is a
  separate, explicitly-approved step, not something this ticket automates
  unattended.*

### Epic 9: Premium — Weekly Planner
- **CROC-025** — Planner schema + CRUD: day × meal-slot (breakfast/lunch/
  dinner) entries for a given week, each optionally pointing at a recipe,
  scoped per user. `GET /planner?week=`, `POST`/`PATCH`/`DELETE
  /planner/entries`, plus a "clear week" bulk action. Gated to
  role ≥ PREMIUM (403 for FREE). *AC: matches the design reference's 7×3
  slot grid (`screenshots/your crockpot/yp2.png`); a slot's recipe must
  already be on the user's menu or favourites — confirm this constraint
  (or its absence) against the design before implementing, don't assume.*

### Epic 10: Premium — Recipe Import via Link
- **CROC-026** — `POST /recipes/import`: accepts a URL, fetches and
  parses it into a draft recipe (name, time, serves, ingredients,
  instructions) for the user to review/edit before saving — not an
  auto-save. v1 implementation is a scraper (no AI dependency required);
  AI-assisted extraction is a possible later swap, not a v1 requirement.
  Gated to role ≥ PREMIUM. *AC: a real recipe URL from a common recipe
  site returns a populated draft; unsupported/malformed URLs return a
  clear 4xx, never a 500.*

### Epic 11: Billing & Subscriptions
*Scheduled roadmap epic, not a "someday" — sequencing after Epics 9-10 is
deliberate (prove the paid features work under manual role grants first),
not a demotion.*
- **CROC-027** — Design the Stripe subscription model: plan/tier mapping,
  webhook events consumed, what `role` transitions look like on
  subscribe/cancel/payment-failure, trial handling if any. Output is a
  design doc + schema addendum ahead of CROC-028.
- **CROC-028** — Stripe Checkout + webhook handling: create checkout
  sessions for the PREMIUM plan, handle `checkout.session.completed` /
  `customer.subscription.updated` / `customer.subscription.deleted`
  webhooks, update `role` accordingly.
- **CROC-029** — Customer portal / cancel flow + billing-history endpoint.

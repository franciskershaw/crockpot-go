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
  repo, not a throwaway external script. Reads a MongoDB Compass JSON
  export (6 collections) from disk — no live Mongo connection, no
  `mongo-driver` dependency in the module graph. Writes through
  **dedicated insert queries, not the API's `repository` Create methods**
  — decided at CROC-024's grill (`docs/handoffs/CROC-024.md`). Those
  methods are shaped for live use (server-assigned id, `created_at =
  now()`, `created_by_name` via a `users` subquery); forcing migration
  through them flattens every recipe's real creation date, which is
  genuine signal (`GET /recipes` sorts `created_at DESC, id`) and
  load-bearing for the ranking work (CROC-042) this migration exists to
  enable. The original intent — migrated rows can't be structurally
  invalid — still holds without them: Postgres enforces every FK
  automatically, and the one application-level check (`item_allowed_units`
  membership per recipe ingredient) is reapplied in the tool. Migrated
  users/recipes/items get a stable UUIDv5 id derived from the Mongo
  `_id` (wipe-and-reload is byte-identical) and, for recipes/items, their
  real `created_at` / `updated_at`. Three `users` are migrated, all
  `ADMIN` — the two real accounts (Francis, Zoe; `google_id` from the
  `Account` collection's Google `sub`, so a dev Google login lands on the
  migrated row) plus a synthetic `Crockpot` account owning the 189
  bulk-seed recipes. The 40 spam/scraper `User` docs, favourites,
  `recipemenus`, `shoppinglists`, and menu history are deferred to later
  passes (need the write endpoints from Epics 5–6). Rerunnable against
  the dev database — the tool truncates its own tables (users, recipes,
  items, and their child/join tables) behind a `MIGRATE_ALLOW` env +
  `--yes` guard, never touching reference / auth tables — so the founder
  always has real data to build the frontend and ranking against; run
  once for real at prod cutover (`--allow-prod`, a separate
  explicitly-approved step). See `docs/handoffs/CROC-024.md` (+ its
  `-data-review` companion) and Epic 8.
- **Auth**: Google OAuth2/OIDC *and* email/password, both issuing the same
  JWT pair as `packing-list-go` — 15 min access token (bearer header,
  stateless), 7-day sliding refresh token (httponly cookie, DB-backed,
  rotates on every use, reuse revokes the whole family). Password accounts
  require email confirmation before first login — a 6-digit OTP code (not
  a link; decided at `CROC-005`, `docs/handoffs/CROC-005.md`, after the
  founder flagged that a clickable confirmation link reads as
  spam/phishing in the wrong context), 10-minute TTL, invalidated after 5
  wrong attempts; forgot-password issues a separate, single-use, expiring
  reset token — decided at `CROC-007` (`docs/handoffs/CROC-007.md`) to be
  an opaque high-entropy link-token (32 random bytes, hex-encoded, sha256-
  hashed like refresh tokens), not the OTP pattern above: a "click here to
  reset your password" link doesn't carry the spam/phishing signal a
  same-session confirmation link does, and the token's entropy makes an
  attempts-lockout unnecessary (`password_reset_tokens` has no `attempts`
  column). A successful reset revokes every other live session for the
  account (`RevokeAllFamiliesForUser`) and auto-issues a fresh one for the
  device that completed it. Password hashing via `golang.org/x/crypto/bcrypt`
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
  one interface split across three tickets: `CROC-004` builds
  `CreateFamily`/`DeleteStaleFamiliesForUser` only; `CROC-007` adds
  `RevokeAllFamiliesForUser` (bulk, revokes every live family for a user
  — password reset logs the account out everywhere except the device
  that just proved it's really them, see `docs/handoffs/CROC-007.md`);
  `CROC-008` adds `RotateFamily`/`FindFamilyByID`/`RevokeFamily`
  (single-family, by-ID, for the rotate-on-use + reuse-detection
  mechanism) to the same interface/struct. Bulk logout-on-reset and
  per-family reuse revocation are different operations, not overlapping
  scope. **A Google sign-in against an email already
  registered via password fails explicitly** (`ErrEmailRegisteredWithPassword`,
  detected as a Postgres unique-violation on `users.email`), redirecting
  the browser to `${FRONTEND_URL}/auth/callback?error=...` rather than
  either merging accounts or surfacing a raw 500 — the direct, foreseeable
  consequence of the "mutually exclusive, no linking" decision above, not
  a hypothetical deferred until password auth ships.
  **A valid, unexpired access/refresh token whose user row no longer
  exists gets `401`, not `500`** — decided at `CROC-009`
  (`docs/handoffs/CROC-009.md`), correcting `CROC-008`'s `RefreshToken`,
  which had masked `models.ErrUserNotFound` behind the same generic `500`
  used for genuine unexpected errors (never a deliberate choice — the
  code just fell into the generic catch-all block). A token that names no
  real user fails to establish identity the same way a missing/malformed/
  expired one does; the enumeration-defense reasoning behind this
  codebase's other generic-error collapses doesn't apply, since the
  caller already possesses a token naming that exact user ID. Applies to
  every authenticated-user-lookup path (`GET /me`, `POST /auth/refresh`).
- **Ownership model**: reference data (`item_categories`, `items`, `units`,
  `recipe_categories`) is system-level, visible to everyone, admin-managed
  only — no per-user copies (unlike `packing-list-go`, which let users
  extend categories/items; Crockpot's reference data is fully curated).
  Recipes have a `created_by_id` and an `approved` flag: creator can always
  see/edit their own unapproved recipes; everyone can see approved ones;
  only ADMIN can flip `approved`, and ADMIN-created recipes are
  auto-approved.
  **Recipe ingredients are catalog references only** (decided at
  `CROC-014`, `docs/handoffs/CROC-014.md`): every `recipe_ingredients`
  row carries a real `item_id` into the curated `items` table — a
  recipe-create payload cannot mint `items` rows, and there is no
  free-text/unstructured ingredient. Rejected: a nullable `item_id` +
  `raw_text` fallback — it would break Epic 6's shopping-list
  aggregation (quantities only sum across recipes when both point at the
  same `item_id`), which is the app's core value proposition. The
  "user needs an ingredient the catalog lacks" gap is closed by
  `CROC-039` (user-suggested items via the same pending-approval model
  as recipes), which keeps every ingredient a real `item_id`. Revisit
  the free-text rejection only if a real recipe class emerges where
  ingredients genuinely can't be normalised.
- **Reference-data access**: reads (`GET /item-categories`, `/units`,
  `/recipe-categories`) are public — no auth middleware — because
  anonymous users browse and filter recipes by category/ingredient.
  Writes are ADMIN-only, gated by `middleware.RequireRole(roles ...string)`
  (route-level, reads the `role` claim `AuthMiddleware` sets). Decided and
  built at `CROC-010` (`docs/handoffs/CROC-010.md`), the first resource
  handler. Rejected: pulling `CROC-023`'s tier-gate helper forward — that
  is the data-dependent recipe-cap check (`role`-keyed limit + count),
  a different shape from a static route gate. Revisit `RequireRole`'s
  signature only when a `≥`-ordering gate is needed (`CROC-025`, planner).
- **Repository pattern, handler structs, black-box handler tests**: same
  conventions as `packing-list-go` (interfaces owned by `handler`,
  `testify/mock` for repo mocks, `httptest` + `gin.New()`).
- **Optional authentication**: `middleware.OptionalAuthMiddleware`
  (`CROC-015`) — parses the bearer token if present and valid and sets
  the same `userID`/`email`/`role` context keys as `AuthMiddleware`, but
  a missing header, a malformed header, or an invalid/expired token all
  fall through to `c.Next()` with no claims (never 401). For public
  reads with a per-caller enhancement (browse shows your own unapproved
  recipes; `CROC-018` will add `isFavourite`). An expired access token
  must not break a browse page — refresh is the frontend's separate
  concern. Handlers read `userIDFromCtx` (already returns `("", false)`
  when unset) to branch anonymous-vs-authenticated.
- **Recipe read model & visibility** (`CROC-015`,
  `docs/handoffs/CROC-015.md`): a recipe is visible to a caller when
  `approved OR created_by_id = caller OR caller_is_admin`. Anonymous →
  approved only; creator → also their own unapproved (immediate
  post-submit feedback, no privacy reason to hide it from its owner);
  ADMIN → everything (makes `CROC-017`'s inline approval work without a
  separate queue endpoint). Same predicate is reused by `CROC-016`
  (edit/delete authz), `CROC-017`, `CROC-018`, and menu/planner reads —
  changing it is a cross-ticket change. `GET /recipes/:id` on a hidden
  recipe returns `404`, identical to a nonexistent id (enumeration
  defence). Two response shapes: a lean **list card** (id, name, image,
  time, serves, approved, `categories:[{id,name}]`, createdAt — no
  ingredients/instructions/notes) and a **detail** DTO (adds
  description, instructions, notes, server-hydrated ingredients with
  item/item-category/unit-abbreviation, createdBy, updatedAt). Diverges
  from `CROC-014`'s create response (`categoryIds` bare IDs) — harmonise
  at `CROC-016`. **Relevance ranking + random ordering + match
  explanation are `CROC-042`, not this predicate's concern.**
- **List pagination**: offset — `?page=` (1-based, default 1) / `?limit=`
  (default 20, max 50), envelope `{recipes, page, limit, total,
  totalPages}`, `total` respects visibility + all filters, over-range
  page → empty `recipes` with a 200. First and only paginated list
  (`CROC-015`); reference-data lists stay bare arrays. Cursor pagination
  rejected — stable `created_at DESC, id` order makes offset correct for
  TanStack `useInfiniteQuery`, and the dataset is ~189 rows. Revisit if
  a list ever needs stable paging under heavy concurrent inserts.
- **Images**: Cloudinary, **signed** client-side direct upload — the
  browser uploads the file straight to Cloudinary using a short-lived
  signature the API issues (`CROC-040`), gets back a `secure_url` +
  `public_id`, and sends only those two strings in the recipe JSON. The
  API never proxies image bytes (keeps the 1 MB JSON body cap
  meaningful) and never ships the Cloudinary API secret to the browser
  (the secret computes the signature server-side, same secret the old
  app's `src/lib/cloudinary.ts` already uses). Unsigned upload presets
  were rejected at `CROC-014`'s grill: extractable from the JS bundle,
  they can't be gated on this app's auth, so anyone could upload to the
  account. Endpoints that accept an image URL (`CROC-014` recipe create,
  `CROC-016` update, `CROC-026` import) validate scheme `https` +
  host `res.cloudinary.com`, not just that it is a well-formed URL —
  otherwise a caller could store an arbitrary third-party URL that every
  viewer's browser then fetches. `CROC-040` adds `CLOUDINARY_CLOUD_NAME`
  config and tightens this to a path-prefix check (this project's cloud,
  not another Cloudinary customer's).
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
short-expiry (verification 10 min, confirmed at `CROC-005`; reset 1h,
confirmed at `CROC-007`).

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
- **CROC-005** — Email/password registration + confirmation. **Done.**
  See `docs/handoffs/CROC-005.md`.
- **CROC-006** — Email/password login. **Done.**
  See `docs/handoffs/CROC-006.md`.
- **CROC-007** — Forgot/reset password (`POST /auth/forgot-password` sends
  a reset-link email; `POST /auth/reset-password` consumes the opaque
  link-token, sets the new password, confirms the email, revokes every
  other live session, and auto-issues a fresh one). Same
  clear-before-insert requirement as CROC-005, against
  `password_reset_tokens`. **Done.** See `docs/handoffs/CROC-007.md`.
- **CROC-008** — Refresh + logout (`POST /auth/refresh`, `POST
  /auth/logout`), rotation + reuse-detection per the design above.
  **Done.** See `docs/handoffs/CROC-008.md`.
- **CROC-009** — `GET /me` profile endpoint (id, email, name, image,
  role); first route to prove `CROC-003`'s `AuthMiddleware` end-to-end.
  **Done.** See `docs/handoffs/CROC-009.md`.
- **CROC-009a** — CORS middleware (single allowed origin `cfg.FrontendURL`,
  credentials, preflight short-circuit; port of `packing-list-go`'s).
  Real cross-origin/browser proof is owed by `crockpot-react` CFE-002a.
  **Done.** See `docs/handoffs/CROC-009a.md`.
- **CROC-044** — Drop user profile images entirely: remove `image` from
  `models.User`, the sqlc `users` queries/generated code
  (`internal/sqlc/queries/users.sql` + regenerated output), the
  repository layer (`GetOrCreateUser`/`refreshLoginProfile`'s
  `avatarURL` param), the Google OAuth claim capture
  (`internal/auth/google.go`'s `IDTokenClaims.AvatarURL`), the `/me`
  response (`CROC-009`), and the `users.image` column via a migration
  (already nullable — no `NOT NULL`/data-shape blocker, but dropping it
  deletes whatever's currently stored). Also update `cmd/migrate-data`'s
  source/transform/load structs, which currently copy it through from
  Mongo. Surfaced at `crockpot-react` CFE-004 (2026-09-04): Google's
  `lh3.googleusercontent.com` avatar URLs 429 unpredictably, even on a
  single first-load request, so `crockpot-react` stopped rendering them
  in favour of initials-only — `image` is now fully unused by the
  frontend. Needs its own grill (drop the column now vs. leave it
  nullable-and-unused for a while; whether `IDTokenClaims.AvatarURL`
  itself is worth deleting or just left uncaptured).

### Epic 3: Reference Data
- **CROC-010** — Item categories CRUD (admin-only writes, public reads).
  Also lands `middleware.RequireRole`, the `handler/{errors,validation}.go`
  helper extraction, and the `item_categories.fa_icon` → `icon` rename +
  real seed data — reused by every Epic 3–6 ticket after it. **Done.**
  See `docs/handoffs/CROC-010.md`.
- **CROC-011** — Units CRUD (admin-only writes, public reads). **Done.**
  See `docs/handoffs/CROC-011.md`.
- **CROC-012** — Items CRUD, with allowed-units association (admin-only
  writes, public reads). First many-to-many join table
  (`item_allowed_units`) and first non-`AuthHandler` consumer of
  `handler.Transactor`. **Done.** See `docs/handoffs/CROC-012.md`.
- **CROC-013** — Recipe categories CRUD (admin-only writes, public reads).
  **Done.** See `docs/handoffs/CROC-013.md`.

### Epic 4: Recipes
- **CROC-014** — Recipe creation (`POST /recipes`, any authenticated
  user). **Done.** See `docs/handoffs/CROC-014.md`. Delivered CROC-023
  (folded in). Load-bearing for later Epic 4 tickets: ingredients are
  catalog references only (see Ownership model above); `recipe_ingredients`
  has a `position` column for submit order; an ingredient `unitId` must
  be in the item's `item_allowed_units` set when that set is non-empty;
  create response is the bare recipe with relations as ID arrays
  (hydration is CROC-015's). Cap TOCTOU gap accepted — revisit at Epic 11.
- **CROC-015** — Recipe read layer: `GET /recipes` (paginated, filtered
  list) + `GET /recipes/:id` (fully-hydrated detail). **Done**
  (2026-08-31, `docs/handoffs/CROC-015.md`). Landed
  `middleware.OptionalAuthMiddleware`, the recipe visibility predicate,
  the list-card / detail DTOs, the filter-param vocabulary, and offset
  pagination (all in Architecture above). Ordering is `created_at DESC`
  only — **relevance ranking + random order + match explanation are
  CROC-042**.
- **CROC-016** — Recipe update/delete (owner or admin only). Open for its
  grill: orphaned Cloudinary images. The old app called
  `deleteRecipeImage(publicId)` server-side on recipe delete / image
  replace; the Go API has no Cloudinary SDK. Decide — a minimal signed
  REST delete call, a periodic sweep, or accept orphans (free-tier
  quota is generous, personal app). Also covers replacing a recipe's
  image on update (same `{url, filename}` validation as CROC-014).
  **Flagged into scope at CROC-015's grill:** (1) a `description` write
  path — CROC-015 reads `description` (returns `null`) but nothing sets
  it; (2) harmonise CROC-014's create response (`categoryIds` bare IDs)
  onto CROC-015's `categories:[{id,name}]` shape.
- **CROC-017** — Admin approval (`PATCH /recipes/:id/approve`, admin-only).
  May add a `GET /recipes?approved=false` admin-only pending-queue filter
  (CROC-015 makes admins see all recipes but adds no focused filter).
- **CROC-018** — Favourites: `POST`/`DELETE /recipes/:id/favourite`
  (idempotent toggle) + `GET /recipes/favourites` (paginated, own
  favourites only). **Done** (2026-09-02, `docs/handoffs/CROC-018.md`).
  Landed full `AuthMiddleware` on all three routes (no anonymous case,
  unlike `GET /recipes`), `recipe_favourites.created_at` for
  favourited-at ordering, and `isFavourite` on `CROC-015`'s
  list-card/detail DTOs.
- **CROC-039** — User-suggested items via pending-approval. Mirrors
  `recipes.approved` onto `items` (`approved` flag + `created_by_id`): a
  user creating a recipe (CROC-014) can mint a *pending* item, usable in
  their own recipe immediately but invisible in everyone else's
  ingredient picker and the global catalog until an admin approves or
  merges it. Needs an `items` migration + an admin approve/merge endpoint
  + picker filtering (approved ∪ own-pending). Does **not** change
  CROC-014's contract — every `recipe_ingredients` row still carries a
  real `item_id`. Rejected alternative: nullable `item_id` + free-text
  `raw_text` on `recipe_ingredients` (guts shopping-list aggregation).
  **Sequencing:** must land before real FREE-user signup opens or the
  frontend recipe-create form ships, else users hit "item not found"
  walls. Until then the founder (admin) adds missing items via CROC-012's
  `/items` API. Grill properly before building — the approve-vs-merge
  UX and near-duplicate handling are open.
- **CROC-040** — Cloudinary signed-upload signature endpoint. `POST
  /uploads/signature`, authenticated: the API computes an HMAC over the
  upload params (timestamp, folder, maybe `public_id`) using the
  Cloudinary API secret held server-side, returns `{signature,
  timestamp, apiKey, cloudName, folder}`; the browser then uploads the
  file straight to Cloudinary with that signature. Keeps image bytes off
  the API and the secret out of the bundle (see the Images architecture
  decision). New config: `CLOUDINARY_CLOUD_NAME`, `CLOUDINARY_API_KEY`,
  `CLOUDINARY_API_SECRET` (the old app's `src/lib/cloudinary.ts` already
  holds real values). **Dependency of** `crockpot-react` CFE-010; CROC-014's
  `image` fields are inert without it. **Sequencing:** before real signup
  opens (same gate as CROC-039); an unsigned preset with Cloudinary-side
  size/format/folder limits is a defensible interim if deferred. Grill
  before building — which params are signed, signature TTL, per-user
  rate limit, whether FREE users may upload at all.
- **CROC-042** — Recipe relevance ranking. Split from CROC-015 at its
  grill (2026-08-31) — the "I have these ingredients, what can I cook?"
  ordering, especially for anonymous users, plus a visible match
  explanation (which ingredients/categories matched). **Full requirements
  + open questions captured in `docs/handoffs/CROC-015.md` appendix.**
  Do **not** port the old app's algorithm — absolute match count ignores
  recipe size, and the 28 flat `recipe_categories` mix dietary / lifestyle
  / cuisine / meal-type kinds that need different ranking weight (probable
  `recipe_categories` schema change). Also owns the low-signal **random
  ordering** (seed mechanism, no Next-style cache). Plugs into CROC-015's
  `ORDER BY` and adds fields to the list-card DTO — additive. Match-
  explanation response fields are **blocked on a design artifact** for
  that UI. Needs its own grill.
- **CROC-043** — Recipe cooking-time bounds. Surfaced at `crockpot-react`
  CFE-004's grill (2026-09-02): the old app got this from a live Prisma
  aggregate (`getRecipes.ts:74-99`, cached hourly); `crockpot-go` has no
  equivalent today. **Blocks** CFE-004's time-slider piece specifically —
  small and isolated enough that the rest of CFE-004 doesn't wait on it.
  **Grilled 2026-09-04 (AI-driven, cheap-to-undo — no separate handoff
  doc):**
  - **Own endpoint, `GET /recipes/time-range`, not folded onto `GET
    /recipes`** — the bounds are fetched once on browse-page mount, not
    on every paginated/filtered list request; folding it in would either
    recompute the aggregate on every call or make the bounds visibly
    jitter as other filters narrow the list, which isn't what a slider's
    range should do. Same fetch-once-and-cache-separately shape as
    `/recipe-categories`/`/items` (CFE-004 decision 9).
  - **Approved-only visibility (`WHERE approved`), not the full CROC-015
    caller-aware predicate** — matches the old app's own
    `getRecipeTimeRange()` exactly (`WHERE approved: true`, no
    per-caller branch). Public route, no `OptionalAuthMiddleware`. Trade-
    off accepted: an admin/creator's own pending recipe's cook time could
    theoretically sit outside the returned range — narrow and rare
    against the complexity of a caller-parameterized query for a control
    whose job is a rough bound, not an exact enclosure.
  - **No server-side cache — live query every request.** No caching
    infrastructure exists anywhere in `crockpot-go` today (checked); the
    old app's "1hr precedent" is `unstable_cache`, a Next.js-specific
    mechanism with no Go equivalent already in place. The aggregate
    itself is trivially cheap (`MIN`/`MAX` over ~213 rows, no join). The
    frontend applies its own long `staleTime` instead, matching the
    pattern CFE-004 decision 9 already uses for `/items`/
    `/recipe-categories`.
  - **`{"minTime": 0, "maxTime": 120}` fallback** when the aggregate is
    `NULL` (zero approved recipes) — same fallback values the old app
    hardcodes for the same empty-aggregate case.
  - **Field names `minTime`/`maxTime`**, not the old app's `{min,max}` —
    matches the name this concept already has everywhere else in both
    codebases (`ListRecipes`/`CountRecipes`'s `min_time`/`max_time` sqlc
    params, `crockpot-react`'s `RecipeListParams.minTime`/`maxTime`);
    `{min,max}` was only ever an artifact of Prisma's `aggregate()` shape.

  Query: `SELECT COALESCE(MIN(time_in_minutes),0)::int AS min_time,
  COALESCE(MAX(time_in_minutes),120)::int AS max_time FROM recipes WHERE
  approved` — one row always returned (bare aggregate, no `GROUP BY`), so
  the fallback lives in SQL, not a Go-side null check.

  **Acceptance criteria**:
  - [ ] `GET /recipes/time-range` → `200 {"minTime": N, "maxTime": M}`,
        no auth required, registered alongside `GET /recipes`/`GET
        /recipes/:id` (same public route group, `main.go`).
  - [ ] Bounds reflect only `approved = true` recipes — an unapproved
        recipe with a cook time outside the approved range never changes
        the response, regardless of caller/auth state.
  - [ ] Zero approved recipes → `{"minTime": 0, "maxTime": 120}`.
  - [ ] `requests/recipes.http` gets a `Time Range` section.
  - [ ] `go test ./internal/handler/...`, `./scripts/test-repo.sh`,
        `golangci-lint run --max-same-issues=0
        --max-issues-per-linter=0 ./...`, `gofmt`, `go vet` all clean.

  **Non-goals**: server-side caching; per-caller/admin-aware visibility;
  wiring the `crockpot-react` slider itself (CFE-004 roadmap step 6,
  separate follow-up once this ships).

  **Verification**: handler tests (mocked repo) assert the response
  shape and the zero-recipe fallback; repository tests against the real
  Neon dev DB (`./scripts/test-repo.sh`) seed an approved + an
  out-of-range unapproved recipe and assert only the approved one is
  reflected, plus the fallback with zero approved rows; `requests/
  recipes.http`'s new section run for real against the local server;
  `/code-review medium main` once green, before close-out.

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
- **CROC-023** — **Delivered by CROC-014** (`docs/handoffs/CROC-014.md`
  decision 1). The recipe-cap limit helper: a `role`-keyed limit lookup
  (`{"FREE": 5}`, absence = uncapped) + owned-recipe count
  (`CountByCreator`, approved + unapproved) backing Epic 4's FREE ≤5 cap
  (`409 recipe_limit_reached`). Scope had already been reduced at
  `CROC-010`'s grill (2026-08-29) to just the data-dependent cap check —
  route-level role gating landed as `middleware.RequireRole` in
  `CROC-010` — and CROC-014's grill folded that residue in rather than
  spend a separate grill/handoff/PR on a ~15-line helper with one
  caller. Number kept for history. PREMIUM `≥`-ordering gates
  (Epics 9–10) extend `RequireRole` when they land. `PRO` stays an
  unused enum value.

### Epic 8: Data Migration
- **CROC-024** — `cmd/migrate-data`: one-off CLI importing the old app's
  users/items/recipes from a MongoDB Compass JSON export. **Done
  (2026-08-31, dev).** Ran 0-skipped: 3 ADMIN users (Francis + Zoe with
  real Google `sub`, a synthetic `Crockpot` owning the seed corpus), 387
  items, 213 recipes. See `docs/handoffs/CROC-024.md` (+ its
  `-data-review` companion) and the "Data migration" architecture bullet.
  - **Not yet run against prod** — a separate explicitly-approved step at
    real cutover, from a *fresh* export (`--allow-prod --yes`).
  - **Still deferred to a later pass**: the 40 spam users, favourites,
    `recipemenus`, `shoppinglists`, menu history.
  - **Delete the tool** (`cmd/migrate-data/` + `internal/sqlc/migrate.sql*`)
    once prod cutover is done and settled — disposal steps in the handoff.

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

### Epic 12: Account Deletion
*Added at `CROC-009`'s grill (`docs/handoffs/CROC-009.md`): raised while
reasoning about what happens to a live token when its user row is gone —
there was no way for a user to delete their own account anywhere in this
spec, not even as a stated non-goal. Design is deliberately deferred to
its own ticket rather than decided inline here — it has real open
questions (hard vs. soft delete, given this spec's current "no soft-delete
requirement identified yet" stance; cascade behaviour for owned recipes/
favourites/menu/shopping-list/planner rows; whether an ADMIN or a user
with pending-approval recipes needs different handling; email confirmation
of intent; session revocation) that deserve their own grill, not a
few-line addendum to a `GET /me` ticket.*
- **CROC-030** — Design + implement self-service account deletion. Output
  of the design half: hard-delete vs. soft-delete decision (and why,
  against this spec's existing no-soft-delete default), what happens to
  the deleted user's recipes/favourites/menu/shopping-list/planner rows,
  confirmation mechanism, and full session revocation on completion.

### Tech Debt & Production Readiness
*From the first whole-codebase tech-debt pass, 2026-08-30. Full detail:
`docs/findings/2026-08-30-tech-debt.md`.*
- **CROC-031** — Fix `PostgresEmailVerificationTokenRepository` to honor
  an active `WithinTx` transaction (matches every other repository's
  `queriesFor(ctx, r.db)` pattern). Not currently exploitable, but a
  silent atomicity trap for the next ticket that wraps it in a
  transaction. Finding 1.
- **CROC-032** — **Done** (2026-08-31). Migration `000008` indexes all
  10 base-schema FK columns that weren't already a leading index column
  (finding 2); `internal/repository/schema_test.go` asserts the property
  via `pg_index`. Plain `CREATE INDEX` (indexes precede any bulk load;
  `CONCURRENTLY` can't run in `golang-migrate`'s per-file transaction).
  Query-shaped composite/covering/partial indexes and `pg_trgm` are out
  of scope — bare FK coverage only. Built ahead of CROC-015.
- **CROC-033** — Decide and implement (or deliberately drop) periodic
  cleanup of stale/expired rows in `refresh_tokens`,
  `email_verification_tokens`, `password_reset_tokens` — currently only
  opportunistic, per-user, on next login. Resolve `lifecycle.go`'s dead,
  stale-against-current-interfaces sweeper code either way. Finding 3.
- **CROC-034** — Add a request body-size-limit middleware — promised by
  `CLAUDE.md`'s folder-layout doc, never built. Finding 4.
- **CROC-035** — Collapse `email.ResendClient`'s two near-identical
  send methods into one shared helper. Finding 5.
- **CROC-036** — `db.go`'s three `fmt.Print*` log lines → `slog`, to
  match the app's structured logger (`main.go:37`). Finding 7. *(Findings
  6 and 8 — rate-limit error-body codes, and the `auth_handler.go`
  helper retrofit — moved to `CROC-041` at that ticket's grill, same
  error-shape theme.)*
- **CROC-037** — Drop the `lib/pq` dependency: switch `db.go`'s migrator
  to `golang-migrate`'s native `database/pgx/v5` driver, reusing the
  app's existing pgx stack. Finding 9.
- **CROC-041** — **Done** (2026-08-31, `docs/handoffs/CROC-041.md`).
  API error responses go through shared helpers in `handler/errors.go`
  (`badRequest`/`notFound`/`conflict`/`unauthorized`/`forbidden`/
  `serverError`; `internalError` for the log-and-500 case), used across
  the whole `handler` package including `auth_handler.go` (also on
  `bindJSON`). `middleware/rate_limit.go`'s two bodies snake_cased to
  `rate_limit_exceeded` / `server_error`. Absorbed tech-debt findings
  6 + 8 (`CROC-036` is now finding 7 only).

*Parked 2026-08-31 — a loosely-scoped idea, not sequenced into a
priority epic yet. Numbered out of physical order deliberately: this
sits conceptually in Epic 6 (Shopping Lists), but the founder wants it
addressed only once Epics 1-6's core functionality has shipped, not
inserted into the current build order. Grill properly before starting —
the open questions below aren't decisions, just what a grill would need
to resolve. Paired with `crockpot-react`'s `CFE-015`.*
- **CROC-038** — "Default items": a per-user saved set of items (e.g.
  toilet paper, eggs, milk) that aren't tied to any recipe or menu,
  addable to the current shopping list individually or in bulk (a
  "restock" gesture distinct from `CROC-022`'s one-off manual add).
  Open for its grill: reuse the `items` reference-data table
  (categorised, admin-curated, shared across users) vs. free-text
  per-user entries; where the saved set lives (a new table vs. a flag
  on existing rows); whether adding a default to the list is "add all"
  or per-item picking; interaction with the FREE/PREMIUM tier split, if
  any.

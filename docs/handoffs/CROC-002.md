# CROC-002 — Initial schema migration

Completed 2026-08-14.

**Implementation mode: AI-driven / spec-driven.** Unlike CROC-001, this
ticket is implemented by Claude following this doc, not hand-written.

## Context

CROC-001 left a placeholder `db/migrations/000001_init.up.sql` /
`.down.sql` pair (comment-only, needed so `go:embed` had a non-empty
directory to compile). This ticket replaces that placeholder's contents
in place with the real v1 schema — every table `docs/specs/master-spec.md`
commits to, up front, per its own "Data migration" reasoning (Epic 8
reads every Mongo collection once and needs somewhere real to land).

No repository/handler code, no `sqlc` queries. Both belong to whichever
ticket first needs them against a real table (see the `sqlc generate`
note below).

## Decisions and why

Claims checked against real source before any of this was decided, not
assumed from precedent:

- **`packing-list-go`'s entire migration history is Google-OAuth-only.**
  No `password_hash` column ever existed there. "Same auth model as
  `packing-list-go`" (master-spec) only covers JWT/refresh-token
  *mechanics* — the password-auth schema below has zero precedent in the
  reference project.
- **The old Next.js app (`crockpot/src/auth.ts`) also has no password
  auth** — Google OAuth + Resend magic-link email, not password+bcrypt.
  So Epic 2's password auth (register/login/forgot-password) is new
  ground, not a port from anywhere.
- **`packing-list-go/docs/handoffs/PACK-027.md`** has the real
  refresh-token rotation/reuse-detection design master-spec already
  commits to reusing — `refresh_tokens.id` *is* the family identifier
  (not a separate `family_id` column), caller-generated because it must
  be known before minting the JWT that embeds it as a `familyId` claim.

**No separate `accounts` table.** Google and password login are
mutually exclusive per user, not linkable — whichever method created the
account is the account. `google_id`/`password_hash` live directly on
`users`, both nullable, with a `CHECK` enforcing exactly one is set:
`CHECK ((google_id IS NOT NULL) != (password_hash IS NOT NULL))`.
Rejected Auth.js-style multi-provider linking (what the old app's
`Account` model does) — real complexity (merge/conflict rules on sign-in,
an extra table) for a convenience this app's scale doesn't need.
Master-spec's original CROC-002 table list named `accounts` — inherited
from the old Prisma model without this decision actually being made;
corrected here. Revisit only if a real need for one person to use both
methods on one account shows up.

**Primary keys: UUID everywhere, with one refinement.** Every table with
its own attributes gets `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`,
matching `packing-list-go` exactly (including keeping the default on
`refresh_tokens.id` even though the app always supplies it explicitly —
consistency over a special case). **Pure bridge tables** —
`item_allowed_units`, `recipe_categories_recipes`, `recipe_favourites`
— have no attributes of their own (unlike `recipe_ingredients`, which
has `quantity`, or `recipe_menu_entries`, which has `serves`), so they
get a composite PK of their two FK columns instead of a surrogate UUID:
nothing ever needs to address one join row individually, and the
composite key enforces uniqueness for free.

**Reference-data delete behavior: `RESTRICT`, not `CASCADE`.**
`items.category_id`, `recipe_ingredients.item_id`/`unit_id`,
`shopping_list_items.item_id`/`unit_id` all `ON DELETE RESTRICT`
(Postgres default). This is shared, admin-curated reference data — an
accidental category delete should fail loudly and force a reassignment,
not silently cascade into real recipes/shopping lists.

**Recipe-referencing delete behavior: `CASCADE`, deliberately the
opposite.** `recipe_menu_entries.recipe_id` and
`menu_history_entries.recipe_id` are `ON DELETE CASCADE`. Recipes are
expected to be deleted by their creators routinely (unlike reference
data); `RESTRICT` here would make any recipe with menu/history activity
permanently undeletable, and there's nothing sensible left to point at
once the recipe is gone.

**`recipe_ingredients.quantity` / `shopping_list_items.quantity`:
`NUMERIC(10,2)`, not `FLOAT`.** Prisma has this as `Float`, but the
shopping-list feature explicitly aggregates quantities across recipes —
repeated floating-point summation is exactly where `0.1 + 0.2`-style
drift produces a subtly wrong shopping-list quantity. Prisma's choice
never had to consider this since Mongo aggregation wasn't part of that
app's design.

**`recipes.created_by_id` is `ON DELETE SET NULL`, plus a
`created_by_name TEXT` snapshot.** Approved recipes are public content
other users may have favourited or menu'd — deleting one user's account
shouldn't cascade-destroy content others depend on, hence `SET NULL`
over `CASCADE`. But `created_by_id` alone loses attribution entirely
once the user is gone, which the founder flagged directly. `FK` can't be
replaced by a name-only field, though — master-spec's ownership model
("creator can always see/edit their own unapproved recipes") is a
permission check against the live `user_id`, which a name string can't
answer, and a display name isn't stable if the user renames themselves.
Resolution: keep the FK for the live ownership check, add
`created_by_name` as a permanent snapshot captured once at recipe
creation, never updated — survives both deletion and later renames.

**`recipes.description TEXT` (nullable) — a field neither reference
project has.** Master-spec's relational mapping (mechanically derived
from `crockpot/prisma/schema.prisma`) never included it because Prisma's
`Recipe` model doesn't have one. Confirmed by reading the actual design
screenshots, not assumed: `screenshots/recipe detail/detail1.png` shows
a genuine italicized headline description under the recipe title,
distinct from "Chef's notes" (a separate bulleted block further down,
which maps to Prisma's `notes`). **Flag for a future ticket**: none of
the `add recipe` mockups (`ar1`–`ar4.1.png`, including the link-import
result) have an input for it yet — the detail page design has outpaced
the add-recipe form design. Not a blocker here, just noted so it isn't
rediscovered as a surprise when that screen gets built.

**`users.email_verified_at TIMESTAMPTZ`, not a boolean.** Records *when*
confirmation happened, which a boolean discards — free information for
free. Google signups get it set immediately at creation (Google already
verified the email); password signups stay `NULL` (blocking login) until
`/auth/confirm` succeeds.

**`users.role` is `TEXT` + `CHECK`, not a native Postgres `ENUM`.**
`packing-list-go` has no tiers at all (no precedent either way); the old
app's `UserRole` enum is Prisma/Mongo-side, not a Postgres construct.
Master-spec itself flags `PRO` as provisional ("revisit if/when such a
feature is designed") — tiers are explicitly not a closed set yet.
`CHECK (role IN ('FREE','PREMIUM','PRO','ADMIN')) DEFAULT 'FREE'` is
cheaper to evolve than `ALTER TYPE`; `sqlc` generates equivalent Go-side
handling either way.

**Reissuing a token must clear the prior row, not just insert — a step
missed on the first pass of this doc.** The partial unique index
(`UNIQUE(user_id) WHERE used_at IS NULL`) only *enforces* "at most one
active token per user per purpose"; it doesn't make reissue work on its
own. Whichever future ticket builds `email_verification_tokens`'/
`password_reset_tokens`' repository must delete (or otherwise clear) any
existing row with `used_at IS NULL` for that user *before* inserting a
new one — covering both "user resends confirmation while the old link is
still live" and "user requests a new link after the old one expired
unused," since the index can't distinguish those cases from each other
(it only looks at `used_at`, not `expires_at`). Without this being
explicit, a user who lets a confirmation/reset link expire unused and
then requests a new one hits a unique-constraint violation instead of
getting a working link — caught by `/code-review` on this ticket's own
diff, not caught while writing the AC the first time.

**`sqlc generate`'s AC does not belong to this ticket, and stops being
pinned to a ticket number at all.** CROC-001's close-out deferred it here
on the assumption real queries would exist once the schema did — untrue:
CROC-002 is the migration file, not repository/query authorship for 19
tables. Writing a query here just to satisfy the AC would repeat the
exact mistake CROC-001's close-out already rejected (a throwaway query
forced into existence), one ticket later. `CROC-003` (JWT helpers) never
touches the DB; `CROC-004` (Google OAuth) is the first ticket with a
genuine query (get-or-create user by `google_id`) — master-spec now
states this as a standing rule instead of a specific ticket guess:
whichever ticket first adds a real query owns proving `sqlc generate`
works against it.

## Acceptance criteria

- [ ] `db/migrations/000001_init.up.sql` / `.down.sql` (in place, not a
      new `000002`) contain the full v1 schema: `users`, `refresh_tokens`,
      `email_verification_tokens`, `password_reset_tokens`,
      `item_categories`, `items`, `units`, `item_allowed_units`,
      `recipe_categories`, `recipes`, `recipe_ingredients`,
      `recipe_categories_recipes`, `recipe_favourites`, `recipe_menus`,
      `recipe_menu_entries`, `menu_history_entries`, `shopping_lists`,
      `shopping_list_items` — no `accounts` table.
- [ ] `users` has the `CHECK` enforcing exactly one of
      `google_id`/`password_hash`, `email_verified_at`, and
      `role TEXT CHECK (...) DEFAULT 'FREE'`.
- [ ] `refresh_tokens` matches `PACK-027`'s shape exactly (`id` is the
      family id, `token_hash`, `previous_token_hash`,
      `previous_token_rotated_at`, `expires_at`, `revoked_at`,
      `created_at`, index on `user_id`).
- [ ] `email_verification_tokens` / `password_reset_tokens` are separate
      tables (not merged), each with a partial unique index
      (`UNIQUE(user_id) WHERE used_at IS NULL`) enforcing at most one
      active token per user per purpose.
- [ ] `item_allowed_units`, `recipe_categories_recipes`,
      `recipe_favourites` use composite PKs of their two FK columns, not
      a surrogate UUID.
- [ ] `items.category_id`, `recipe_ingredients.item_id`/`unit_id`,
      `shopping_list_items.item_id`/`unit_id` are `ON DELETE RESTRICT`.
- [ ] `recipe_menu_entries.recipe_id`, `menu_history_entries.recipe_id`
      are `ON DELETE CASCADE`.
- [ ] `recipe_ingredients.quantity`, `shopping_list_items.quantity` are
      `NUMERIC(10,2)`.
- [ ] `recipes` has `created_by_id FK ON DELETE SET NULL`,
      `created_by_name TEXT`, and `description TEXT` (all beyond the
      direct Prisma mapping, per the decisions above).
- [ ] `recipe_menus.user_id` / `shopping_lists.user_id` are `UNIQUE`
      (one row per user).
- [ ] `migrate -path db/migrations -database "$DATABASE_URL" up`, `down`,
      `up` all exit clean against the real Neon dev DB.
- [ ] `go run .` boots successfully against the same dev DB (proves the
      app's own boot-time migration path, not just the CLI).

## Non-goals

- No `sqlc` queries, no repository code — first real consumer's job
  (see the standing rule above).
- No `accounts` table / account linking (see Decisions).
- No admin-panel schema — v1 has no dedicated admin tables per
  master-spec's non-goals.

## Verification modes

- **Service/API boundary**: `migrate up` / `down` / `up` against the
  real Neon dev `DATABASE_URL`, all exiting clean — exercises both
  migration directions, which `go run .` alone can't (`db.InitDB` only
  ever calls `.Up()`). Then `go run .` against the same DB, confirming
  the app's boot-time path also applies cleanly.
- **Limits/config**: a real insert/select through `psql` (or a throwaway
  query) against a `NUMERIC(10,2)` column with a fractional value (e.g.
  `2.5`), confirming exact round-trip — proves the precision decision,
  not just that the column exists.
- **Code review**: `/code-review` on the migration file (and any
  `sqlc.yaml`/doc changes) before close-out.
- **Tests**: none — SQL + docs only, no Go logic in this ticket.
  Matches `packing-list-go`'s own precedent (schema migrations there
  carry no Go tests; the repository-layer tests that exercise a schema
  belong to whichever ticket first builds a repository against it).
- **Lint**: N/A — no Go files change in this ticket.

# CROC-017 — Admin recipe approval

`PATCH /recipes/:id/approve` (admin-only) approves a pending recipe, but
only if it hasn't changed since the admin loaded it. `GET
/recipes?approved=false` lists pending recipes and backs the admin's
Pending queue. Approval is one-way. Grilled 2026-10-09 together with
`crockpot-react` `CFE-014` (`docs/handoffs/CFE-014.md` there). Cheap to
undo.

**Implementation mode**: AI-driven.

## Facts this rests on (checked 2026-10-09)

- No approve route exists. The authenticated `/recipes` group
  (`main.go:213-222`) has create/update/delete/favourites only. Admin-only
  groups use `AuthMiddleware` + `RequireRole("ADMIN")`
  (`recipeCategoriesAdmin`, `main.go:202-207`).
- Visibility (`CROC-015`/`CROC-065`): admins see every recipe,
  non-admins see approved recipes plus their own. `?mine=true` is parsed as
  `c.Query("mine") == "true"` (`internal/handler/recipe_requests.go:316`).
- Unfiltered browse is seeded-random (`md5(id || seed)`,
  `internal/sqlc/queries/recipes.sql:159-161`); filtered lists fall back to
  `created_at DESC, id`.
- `GetRecipeForWrite` (`recipes.sql:28-33`) is `FOR UPDATE` and selects
  `id, created_by_id, approved, image_url, image_filename`, with no
  `updated_at`.
- `UpdateRecipe` sets `updated_at = now()` on every owner/admin edit,
  photo included (`recipes.sql:54-67`). The detail DTO exposes
  `updatedAt` (`internal/models/recipe.go:57`).
- `recipeWriteError` (`internal/repository/recipe.go:627`): admins may
  write any recipe, so admin delete already exists as "reject".
- Old app had approve + reject (reject = `approved: false`) + bulk
  (`crockpot/src/actions/recipes.ts:274-304`).

## Decisions

1. **One-way.** No un-approve and no rejected state. An approved recipe
   can already be on other users' menus, shopping lists and favourites,
   and hiding it would strand those rows. Rejecting is admin delete, which
   already resyncs menu holders. Fixing is admin edit, then approve. The
   creator gets no rejection feedback: accepted while there's one admin
   and no notifications. Revisit if signup opens wide, with a separate
   pending → rejected state and a reason, never un-approval.
2. **Stale-view guard.** The body carries the `updatedAt` the admin's page
   loaded. Under `GetRecipeForWrite`'s row lock, a mismatch returns `409
   recipe_changed`. Without it, an owner swapping the photo after the
   admin loaded the page gets an unseen photo approved, and CROC-062 then
   locks the owner out of changing it.
3. **Approval doesn't bump `updated_at`.** `updatedAt` stays the content
   version. No `approved_at` column. Add one later (non-breaking) if
   anything needs it.
4. **Already approved → 200 no-op**, whatever `updatedAt` says. Approved
   recipes are admin-edit-only, so there's nothing unseen to guard, and a
   double-click or second tab shouldn't error.
5. **Pending filter**: `approved=false` on the existing `GET /recipes`,
   parsed like `mine` (only the literal `"false"` filters, anything else
   is ignored). No admin gate: visibility already narrows a non-admin to
   their own pending recipes. Counts as a filter, so it orders `created_at
   DESC` rather than random. Pagination/envelope/`total` unchanged.
6. **No bulk approve, no admin table.**

## Contract

`PATCH /recipes/:id/approve`, body `{"updatedAt": "<RFC3339>"}`.

| Case | Response |
|---|---|
| Pending, `updatedAt` matches | `200` detail DTO, `approved: true` |
| Already approved | `200` detail DTO, unchanged |
| `updatedAt` mismatch on a pending recipe | `409 {"error":"recipe_changed"}` |
| Body missing/malformed `updatedAt` | `400 invalid_request` |
| Malformed id | `400 invalid_request` (`parseID`, as every `/:id` route) |
| No token | `401` |
| Non-admin | `403` (`RequireRole`) |
| Unknown id | `404 not_found` (the code every recipe 404 uses) |

## Acceptance criteria

- [ ] Repo `ApproveRecipe(ctx, id, seenUpdatedAt)`, in one tx under
      `GetRecipeForWrite`'s lock (query gains `updated_at`): pending +
      match → `approved = true`, `updated_at` unchanged; already approved
      → no write, no error; mismatch → `models.ErrRecipeChanged`; unknown
      → `ErrRecipeNotFound`.
- [ ] A repo test round-trips `updatedAt` through JSON (`GetRecipe` →
      marshal → unmarshal → `ApproveRecipe`) against Neon and matches, so
      microsecond precision survives.
- [ ] Lock test: an owner `UpdateRecipe` tx holding the row makes a
      concurrent approve wait (`waitForLockWait`, CROC-058 pattern), then
      return `ErrRecipeChanged`. Seen failing with the lock removed.
- [ ] `ListRecipes` with `approved=false`: admin gets every pending recipe
      and no approved ones; a non-admin gets only their own pending
      recipes; `total` matches; order is `created_at DESC`.
- [ ] Handler: every row of the contract table, including the 400 for a
      missing body and a non-RFC3339 string.
- [ ] Route registered in a new admin `/recipes` group; mocks regenerated
      (`go tool mockery`).
- [ ] Spec endpoint docs updated. `requests/recipes.http` gains an Approve
      section: admin approves a pending recipe (200), repeats (200), stale
      `updatedAt` (409), non-admin (403); and a `?approved=false` list as
      admin and as the owner.

## Non-goals

- Un-approve, reject state, rejection reason, notifying the creator.
- Bulk approve; an admin dashboard.
- Image moderation (Cloudinary add-on noted under `CFE-014`).
- `approved_at` / approved-by audit.

## Pieces

1. **Repo: approve** — `ErrRecipeChanged`, `updated_at` on
   `GetRecipeForWrite`, `ApproveRecipe` query + method, repo tests incl.
   round-trip and lock test. Red, stop, green, stop.
2. **Repo: pending filter** — `Approved` filter on `ListRecipes`, repo
   tests. Red, stop, green, stop.
3. **Handler + route** — `Approve` handler, `approved` query param,
   route, mocks, handler tests, spec + `.http`. Red, stop, green, stop.

## Verification

- **Logic** (failing tests first): `./scripts/test-repo.sh -run
  'TestApproveRecipe|TestListRecipes'`, `go test ./internal/handler/...`.
- **API boundary** (real request per piece 3): run the Approve and
  pending-list sections of `requests/recipes.http` against the local API
  on Neon, with an admin and a FREE test account; confirm the 409 by
  editing the recipe as the owner between the GET and the PATCH.
- Full repo suite green against Neon before close-out; lint uncapped.

Completed 2026-10-09.

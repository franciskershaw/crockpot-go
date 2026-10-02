# CROC-040 — Recipe photos: proxied upload to Cloudinary + cleanup

**Implementation mode: AI-driven.**

## Summary

Recipe create/update become `multipart/form-data`: a `recipe` JSON part
(today's fields) plus an optional `photo` file. The API checks the file,
uploads it to Cloudinary server-side into this environment's folder, then
runs the existing transaction; the client never sends an image URL. When
a photo is replaced, removed or its recipe deleted, the old asset is
destroyed after commit, unless another recipe still uses it. Grilled
2026-10-02. Expensive to undo: it reverses the "API never proxies image
bytes" decision, changes the recipe write contract `crockpot-react`
`CFE-049` builds on, and destroys assets irreversibly.

## Facts checked before deciding

- Migrated images (`crockpotV3.Recipe.json`): 213 recipes, all with an
  image, all on cloud `dqdjr1d4f`. Folders `Crockpot/` ×189 (uploaded
  2022-10 → 2025-05, original app) and `recipes/` ×24 (2025-12 → 2026-08,
  the latest rebuild). Every row's `filename` equals the public_id parsed
  from its URL (`/image/upload/v<n>/<public_id>.<ext>`); no image is
  shared between recipes.
- The old app uploaded through its own server (`crockpot/src/lib/cloudinary.ts`
  `uploadImage`), allowed jpeg/png/webp ≤5 MB, destroyed the image on
  recipe delete best-effort (`crockpot/src/lib/upload-helpers.ts`), and
  served sized `f_auto,q_auto` URLs via `next-cloudinary`.
- Measured 2026-10-02: originals 479 KB / 1.42 MB / 88 KB vs 48 / 19 /
  39 KB at `f_auto,q_auto,c_fill,w_600,h_400`. `crockpot-react` renders
  raw `imageUrl` today (`RecipeCard.tsx:55`, `MobileRecipeRow.tsx:59`,
  `RecipeHero.tsx:65`) — `CFE-049`'s job.
- Cloudinary docs: signed requests sign every param except `file`,
  `cloud_name`, `resource_type`, `api_key`, valid one hour; `overwrite`
  defaults true for signed uploads; no max-size upload param; `folder` is
  fixed-folder-mode only, dynamic mode uses `asset_folder` (+
  `use_asset_folder_as_public_id_prefix`); destroy is `POST
  /v1_1/<cloud>/image/destroy` with `public_id`, optional `invalidate`,
  returns `{"result":"ok"}`.
- Body cap is global: `server.Use(middleware.BodySizeLimit(maxRequestBodyBytes))`
  (`main.go:100`, 1 MiB at `:32`).
- Rate limiter keys on client IP (mgin default key getter,
  `internal/middleware/rate_limit.go:25`).
- Third-party client precedent: `email.ResendClient` (10 s
  `http.Client` timeout, `internal/email/resend.go`) consumed through the
  handler-defined `EmailSender` (`internal/handler/auth_handler.go:82`).
- `GetRecipeForWrite` is `FOR UPDATE` and selects only `id,
  created_by_id, approved` (`internal/sqlc/queries/recipes.sql:30`);
  `recipeWriteError` holds the 404/403/approved-lock rule
  (`internal/repository/recipe.go:511`); `withinRecipeCap` runs before the
  transaction on create (`internal/handler/recipe_handler.go:62`).
- All create/update handler tests go through `doRecipeCreate` /
  `doRecipeUpdate` (`internal/handler/recipe_handler_test.go:111`, `:129`).

## Decisions

1. **Proxied save, not browser-direct signed upload.** At this scale
   (one droplet, a few hundred recipes, rare single-photo uploads) the
   offloading benefit of direct upload is near zero, while its
   distributed trust created most of the design work: signature reuse,
   clients adopting other recipes' URLs, uploads abandoned before save.
   Proxying puts every rule in the API, makes recipe + photo succeed or
   fail together, and gives `CROC-026` import the same upload path it
   needs anyway. Cost: a second network hop (~0.3–0.8 s) and a scoped
   body-cap exception. Revisit if uploads become heavy (multiple photos
   per recipe, video, many active users) — the multipart contract can
   stay while the transport moves to direct upload.
2. **Multipart-only** on `POST /recipes` and `PATCH /recipes/:id`:
   required `recipe` part (JSON, today's fields), optional `photo` file
   part. No JSON path — a second contract the app never uses would have
   to be kept identical by hand. The request `image` field is removed;
   unknown fields are not an error, so a stale `image` is ignored.
3. **Photo semantics.** PATCH: `photo` replaces, `"removeImage": true`
   removes, neither keeps the current image; both → 400 `invalid_image`.
   POST: `photo` or no image; `removeImage` → 400 `invalid_image`. The one
   deliberate exception to PATCH full-replace: the client can't restate a
   URL it may no longer send.
4. **File checks:** type sniffed from content (`http.DetectContentType`),
   jpeg/png/webp only → else 400 `invalid_image`; >5 MB → 400
   `image_too_large`. Recipe create/update get a 6 MiB body cap; every
   other route keeps 1 MiB (the global middleware moves off those two
   routes). Over the cap → existing 413 `request_too_large`.
5. **Upload params (server-side):** public_id `<folder>/<uuid>`,
   `overwrite=false`, `allowed_formats=jpg,jpeg,png,webp`, incoming
   transformation `c_limit,w_1600,h_1600`. Folder vs `asset_folder`
   follows the account's folder mode (piece 1 checks it); either way the
   public_id must start with the folder.
6. **Folder per environment:** `CLOUDINARY_UPLOAD_FOLDER` (`recipes` in
   production, e.g. `dev/recipes` locally). The dev DB holds the same
   migrated rows as production against one Cloudinary account, so
   unscoped cleanup in dev would destroy production's photos.
7. **Hard cutover:** the old app goes offline at launch after a final
   `cmd/migrate-data` run, so production may destroy legacy assets from
   day one. No legacy-delete flag.
8. **No client URLs, so no URL parsing.** The server generates every
   new public_id; `image_filename` is server-written or migrated (all 213
   match their URLs). Store Cloudinary's `secure_url` as `image_url` and
   the generated public_id as `image_filename`; response shapes
   unchanged. `validateRecipeImage` (`internal/handler/recipe_requests.go:318`)
   is deleted with the request `image` field (piece 6). (Amended
   2026-10-02 at piece 3: the parse/allowed-to-store rules were carried
   over from the signed design and had no caller.)
9. **Order of a save with a photo:**
   1. parse + validate the recipe and the file;
   2. pre-check permission outside the transaction — PATCH: the
      `recipeWriteError` rule on a non-locking read; POST:
      `withinRecipeCap` — so refusals never upload;
   3. per-user photo limit (decision 11);
   4. upload, 20 s timeout; failure → 502 `image_upload_failed`, nothing
      saved;
   5. the existing transaction, which re-checks permission under the
      row lock (the pre-check is an optimisation, not the guard);
   6. after commit, cleanup (decision 10).
   Never upload inside the transaction: it would hold the `FOR UPDATE`
   lock and a pooled Neon connection across a network call.
10. **Cleanup.** Decided in the transaction: the old public_id (from
    `GetRecipeForWrite`, which gains `image_filename`), skipped if any
    other recipe still references it, and only under folders this
    environment owns (its upload folder; legacy folders in production
    only). Run after commit, synchronously, with
    `context.WithoutCancel`, the client timeout and `invalidate=true`.
    Failure logs and never fails the request. **Compensation:** a
    transaction that fails after a successful upload destroys the new
    asset immediately, same rules. Accepted: a third party adopting the
    exact URL between commit and destroy loses it (sub-second window,
    harms only the adopter); a crash between upload and commit orphans
    one asset.
11. **Per-user photo limit: 20/hour**, checked in the handler only when a
    `photo` part is present, via a `ulule/limiter` instance keyed by user
    ID; 429 `rate_limit_exceeded` + `Retry-After` from a helper shared
    with `RateLimitMiddleware`. Saves without a photo are only under the
    global 120/min/IP. Any authenticated user may upload, FREE included.
12. **`ImageStore` interface** in `internal/handler` (`Upload`,
    `Destroy`), implemented by a Cloudinary client modelled on
    `email.ResendClient` (signed API requests, own `http.Client`),
    mockery mock for handler tests.
13. **Config:** `CLOUDINARY_CLOUD_NAME`, `CLOUDINARY_API_KEY`,
    `CLOUDINARY_API_SECRET`, `CLOUDINARY_UPLOAD_FOLDER` all required in
    `config.validate`, no folder default — a missing var fails startup
    rather than uploading into the wrong folder.

## Consequences outside this repo

- **`crockpot-react` breaks locally on merge:** CFE-010's JSON saves
  get 400 `invalid_request` until `CFE-049`'s first piece switches saves to
  `FormData`. Nothing is released; expected, not a bug.
- **nginx on the droplet:** default `client_max_body_size` is 1 MB —
  raise it for this API (≥6 MB) at deploy, or every photo save 413s
  before reaching Go.
- `CFE-049` owns browser shrink, upload-on-Save, sized delivery URLs,
  and the `FormData` rework of `toRequest`/`fromDetail`/`api.ts`.
- `CROC-026` import re-hosts the scraped image through the same
  `ImageStore.Upload`.

## Non-goals

- A sweep for orphaned assets (crash between upload and commit). If
  orphans show up, the safe design is tag-pending-then-sweep.
- Moving or renaming legacy assets.
- Server-side resizing beyond the incoming `c_limit` transformation;
  delivery sizing is the frontend's.
- Multiple photos per recipe.

## Acceptance criteria

- [ ] Startup fails if any `CLOUDINARY_*` var is missing.
- [ ] `POST`/`PATCH /recipes` accept only multipart with a `recipe`
      part; a JSON body or missing/malformed `recipe` part → 400
      `invalid_request` (`bindJSON`'s code, `internal/handler/validation.go:14`).
- [ ] A jpeg/png/webp ≤5 MB `photo` is stored under
      `<CLOUDINARY_UPLOAD_FOLDER>/<uuid>`, ≤1600 px, and returned as
      `imageUrl`.
- [ ] Non-image content (whatever its declared type) → 400
      `invalid_image`; >5 MB → 400 `image_too_large`; >6 MiB body → 413.
- [ ] PATCH: no photo + no flag keeps the image; `removeImage` removes
      it; photo replaces it; both → 400. POST with `removeImage` → 400.
- [ ] A PATCH with a photo on a recipe the caller can't write returns
      its 403/404/locked error and uploads nothing.
- [ ] Cloudinary failure/timeout → 502 `image_upload_failed`, nothing
      saved.
- [ ] Transaction failure after upload destroys the new asset.
- [ ] Replace, remove and delete destroy the old asset after commit with
      `invalidate=true`; not when another recipe references it; never
      outside the folders the environment owns; a destroy failure still
      returns success.
- [ ] 21st photo save in an hour by one user → 429 with `Retry-After`;
      saves without a photo are unaffected.
- [ ] Every other route still rejects bodies over 1 MiB.
- [ ] `requests/recipes.http` creates, replaces, removes and deletes
      with a photo (multipart), plus a non-image and an oversize case.

## Pieces and verification

Each piece: failing tests first, stop at red, then green, then stop.

1. **Folder-mode check** — one real Admin API call with the dev
   credentials; record the mode here before piece 4 relies on it.
   (API boundary) **Done 2026-10-02:** `GET /config?settings=true` →
   cloud `dqdjr1d4f`, `folder_mode: fixed`. Uploads send only
   `public_id=<folder>/<uuid>` (the path is the folder); no `folder` or
   `asset_folder` param.
2. **Config** — four required vars. `config_test.go`. (logic)
3. **Destroy scope** — `handler.ImageScope{UploadFolder, Production}`
   `.CanDestroy(publicID)`: true only under `UploadFolder/`, or under
   `Crockpot/` / `recipes/` when `Production`. Table tests: dev vs prod,
   legacy folders, prefix near-misses, empty. (logic)
4. **Cloudinary client** (`ImageStore`): signed upload with decision 5's
   params, destroy with `invalidate`. Unit tests against an
   `httptest.Server` for the signature and form fields; then one real
   upload and destroy against the dev folder, checked in the console.
   (logic + API boundary)
5. **Body cap per route** — 6 MiB on recipe create/update, 1 MiB
   elsewhere. Middleware/router tests. (logic)
6. **Multipart request + file checks** — `recipe` part parsing, photo
   sniff/size, `removeImage` rules; `doRecipeCreate`/`doRecipeUpdate`
   become multipart. Handler tests. (logic)
7. **Save flow** — pre-check, photo limit (shared 429 helper), upload,
   transaction, compensation, post-commit cleanup; repository returns
   the public_id to destroy; still-referenced query. Handler tests with
   the `ImageStore` mock; repository tests on Neon. (logic)
8. **`.http` walkthrough** — `requests/recipes.http` multipart cases run
   top to bottom against a local server with real Cloudinary; confirm
   each asset appears/disappears in `dev/recipes/` in the console;
   21st-photo 429; time one real save with a photo. (API boundary +
   limits)

Commands: `go test ./internal/handler/...`, `./scripts/test-repo.sh`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`,
then `branch-review` before close-out.

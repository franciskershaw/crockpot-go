# CROC-045 — Gzip API responses

Every JSON response is gzip-compressed when the client accepts it, through
`github.com/gin-contrib/gzip` as global middleware. This replaces the
ticket's original plan to paginate `GET /items`. Re-scoped 2026-10-03
during `crockpot-react` `CFE-047`'s grill; fully specified, so no separate
grill. Cheap to undo.

**Implementation mode**: AI-driven.

## Facts this rests on (checked 2026-10-03)

- The app is not deployed. Production is the old Next.js/MongoDB app, and
  every slowness observation is local dev. There's no nginx or proxy yet to
  compress, so it has to happen in this server.
- `GET /items` returns all items unpaginated (`item_handler.go:40-46`), in
  two queries with no N+1 (`repository/item.go:33-58`). There are 387 items
  (`crockpotV3.Item.json`). The browser shows 126 KB uncompressed against
  3.3 KB for item categories.
- No compression anywhere: no `gzip` in any `.go` file or `go.mod`.
- Middleware is registered in `main.go:106-117` (`gin.Default()`, then
  CORS, body-size limit, rate limit).
- The Go server never serves image bytes: photos are proxied to Cloudinary
  (`main.go:95-98`), and responses carry URLs. No route needs excluding.
- Every `crockpot-react` items consumer searches the whole list in memory:
  `FilterOptionList` (browse filter), `AddItemSearch`'s `searchItems`
  (recipe form and shopping list), `FilterPills` name lookup,
  `IngredientsSection`'s `itemsById`.

## Decisions

1. **Compress, don't paginate.** Pagination only works if every consumer
   above moves to server-side search, and typeahead becomes a request per
   keystroke on the same slow links. The problem is bytes on slow
   connections, which gzip cuts by about 70% on every endpoint with no
   contract change. Revisit pagination if the catalogue grows to the point
   where the gzipped `/items` is still slow on a throttled connection.
2. **In the Go server, not a proxy.** Nothing is deployed and nginx config
   doesn't exist, so the Go server is the only place it can be checked
   today, and it keeps working behind whatever proxy comes later. nginx
   passes an already-encoded response through untouched.
3. **`gin-contrib/gzip`, not a stdlib wrapper.** It's maintained by the gin
   org and handles `Accept-Encoding` negotiation, `Vary`, and
   double-compression. A hand-written wrapper is about 40 lines plus its own
   tests for each of those. Confirm its current API and the `go.mod` version
   while building; don't rely on the ticket's description of it.
4. **Global, at `gzip.DefaultCompression`, with no excluded paths**
   (decision 2's Cloudinary fact). Registered alongside the existing
   `server.Use` calls. Before placing it, read how gin-contrib/gzip
   interacts with the rate limiter's and CORS's early-abort responses.

## Non-goals

- Pagination or server-side search of `/items`.
- Trimming fields from the item DTO.
- Request-body decompression.
- Diagnosing the old `/recipes?page=3` 500 noted with the original ticket
  (unconfirmed cause, local dev).

## Acceptance criteria

- [x] A request with `Accept-Encoding: gzip` gets `Content-Encoding: gzip`,
      `Vary: Accept-Encoding`, and a body that decompresses to the same JSON.
- [x] A request without `Accept-Encoding` gets an uncompressed body as today.
- [x] Error responses (`400`/`401`/`429`) still reach the client intact,
      CORS headers included.
- [x] `/items` compressed size is recorded against the uncompressed size:
      125,203 B → 15,847 B (−87%); `/item-categories` 3,700 B → 1,257 B.

## Pieces and verification

One piece: failing tests first, stop at red, then green, then stop.

1. **Gzip middleware** (logic + API boundary). Handler/router-level test:
   with `Accept-Encoding: gzip` the response is gzip-encoded and decodes to
   the expected JSON; without it, the response is plain. Then a real request
   against a local server:
   `curl -s -H 'Accept-Encoding: gzip' -o /dev/null -w '%{size_download}\n' localhost:8080/items`
   compared against the same request without the header. Run
   `lsof -iTCP:8080 -sTCP:LISTEN` first, because another session may be
   running an older build. Add a gzip check to `requests/items.http`.

Commands: `go test ./...`, `./scripts/test-repo.sh`,
`golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`,
then `branch-review` before close-out.

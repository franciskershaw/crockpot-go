# Manual `.http` regression suite

One file per resource, run top-to-bottom in VS Code with the [REST
Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client)
extension. Mirrors `packing-list-go/requests/README.md`'s convention —
this is the manual regression pass for the whole API, not something run
on every commit.

## Setup

1. Start the server: `go run main.go`

That's it for now — `register`/`confirm`/`resend-confirmation` are all
unauthenticated by definition, so there's no token to seed for those.
`Login` (`CROC-006`) captures the access token via REST Client's
response-variable syntax; `refresh`/`logout` (`CROC-008`) chain directly
off `Login`'s `Set-Cookie` response instead of a separately-seeded
token — REST Client's cookie jar carries it forward automatically. See
`CLAUDE.md`'s "Token acquisition" note for the full reasoning.

The host resolves from `.env`, reusing the same `PORT` the server binds
to (`config.Config`) — every request URL is
`http://localhost:{{$dotenv PORT}}/...`.

## Running a file

Run top-to-bottom, once, per session. Sections chain off `@name`-captured
variables from earlier in the same file where relevant. Each file ends
with a **Cleanup** section that removes anything real it created (e.g. a
test user row), so a re-run from a clean server behaves the same way
twice.

**Variables and cookies don't cross files** — REST Client scopes both
per `.http` file, so a file exercising a protected route can't chain off
another file's `Login`. Each such file (e.g. `me.http`) does its own
short `Login` at the top instead, against the same confirmed password
account `auth.http` sets up — run `auth.http`'s ✅ register + ✅ confirm
sections once first on a fresh dev DB if that account doesn't exist yet.

## ADMIN-gated routes

`item-categories.http`'s writes (`POST`/`PATCH`/`DELETE`) are gated by
`middleware.RequireRole("ADMIN")`, and the dev DB has no ADMIN user by
default — bump the shared test account once:

```
UPDATE users SET role = 'ADMIN' WHERE email = '<the shared test account>';
```

Harmless to `auth.http` / `me.http`, neither asserts on `role`. Any
future ADMIN-gated resource file reuses the same bumped account.

## What's deliberately not covered

- **Google login/callback** — needs a real browser round-trip, can't be
  driven by a plain `.http` request. Same reasoning as
  `packing-list-go/requests/README.md`.
- **Real email delivery** — sending a real code via Resend and receiving
  it is verified manually, outside this file, per ticket (see each
  ticket's handoff doc). `.http` requests here exercise the API contract,
  not the inbox.

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

## What's deliberately not covered

- **Google login/callback** — needs a real browser round-trip, can't be
  driven by a plain `.http` request. Same reasoning as
  `packing-list-go/requests/README.md`.
- **Real email delivery** — sending a real code via Resend and receiving
  it is verified manually, outside this file, per ticket (see each
  ticket's handoff doc). `.http` requests here exercise the API contract,
  not the inbox.

# Manual `.http` regression suite

One file per resource, run top-to-bottom in VS Code with the [REST
Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client)
extension. Mirrors `packing-list-go/requests/README.md`'s convention —
this is the manual regression pass for the whole API, not something run
on every commit.

## Setup

1. Start the server: `go run main.go`

That's it for now — `requests/auth.http`'s current sections
(`register`/`confirm`/`resend-confirmation`) are all unauthenticated by
definition, so there's no token to seed. Once `CROC-006` (password login)
and `CROC-008` (`/auth/refresh`) land, this file gains token-acquiring
chains the same way `packing-list-go`'s does — see
`docs/handoffs/CROC-005.md`'s "Token acquisition" note in `CLAUDE.md` for
the planned shape.

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

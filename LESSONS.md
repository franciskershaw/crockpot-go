# Lessons

Running retro log for this repo. One entry per ticket close-out: what
caused rework (if anything), what pattern should become a standing rule,
and whether this file or the project's own `CLAUDE.md` needed a new line
as a result. Reviewed at the start of every new ticket's `grill-me` and at
project kickoff.

## 2026-08-11 — Kickoff

Project set up via `project-kickoff`. Master spec and ticket backlog
written against the existing `crockpot` (Next.js/Prisma/MongoDB) app as
the functional reference and `packing-list-go` as the architectural
reference (repository pattern, JWT/OAuth model, Gin, golang-migrate,
testing conventions all reused deliberately). Two decisions departed from
straight reuse: Postgres instead of Mongo (relational schema replaces
Mongo's embedded documents/array-of-ids many-to-many pattern), and sqlc on
top of pgx instead of `packing-list-go`'s raw hand-scanned pgx (Crockpot's
schema is roughly 2x the table count).

First spec draft undersold two things, caught in a second pass before any
code was written: billing was framed as "design only, no implementation,"
which read as shelved rather than scheduled — reworded into a real epic
(Epic 11) sequenced after the PREMIUM features it gates, not dropped. And
there was no plan at all for getting existing Mongo data into the new
schema, including for local dev — added as its own epic (Epic 8,
`cmd/migrate-data`, rerunnable against dev, one real run against prod at
cutover) once raised. Lesson: kickoff's "non-goals" section is a place
scope quietly leaks out — read it back to the person, not just written by
default from what's easy to defer. The PREMIUM/PRO tier split (what's
free forever vs. paid, whether PRO is worth having yet) also needed an
actual discussion rather than a single confirm — recorded under "Tiers"
in `docs/specs/master-spec.md`. No code written yet — nothing to retro on
implementation quality until CROC-001 lands.

## 2026-08-14 — CROC-001 — Scaffold, config, DB, server bootstrap landed

- `go mod tidy` run mid-ticket (fixing an unrelated indirect-flag oddity)
  pruned `godotenv` before it was ever used, causing a confusing later
  "package not found" once `main.go` actually needed it.
- End-of-ticket `/code-review` caught a real bug (`WriteTimeout` 15s vs.
  `shutdownGracePeriod` 10s) and a self-authored violation of this
  project's own "plans, not narrated history" rule in `master-spec.md` —
  neither would've surfaced without a dedicated pass.
- **Pattern**: hand-written-mode tickets fall back to Claude-written code
  fastest on pieces with no reference-project precedent (`pgxpool` had
  none in `packing-list-go`) — expected, not a mode failure.
- Noted, not acted on: `/code-review`'s time/token cost felt high
  relative to the diff size reviewed — worth another look if it recurs.

## 2026-08-14 — CROC-002 — Full v1 schema migration landed

- `golang-migrate`'s CLI "nothing applied" sentinel is `force -- -1`, not
  `force 0` — `0` is treated as a real (nonexistent) migration version,
  and a bare `-1` gets eaten by the flag parser without `--`. Two failed
  attempts before finding this.
- `/code-review` caught a real gap between what was agreed in
  conversation (token reissue must clear the prior row before inserting
  a new one) and what actually made it into the written handoff doc —
  second ticket in a row this exact shape of miss has happened
  (CROC-001's was the master-spec narration violation).
- **Pattern**: write a decision into the doc in the same turn it's
  agreed — don't let the conversation carry it and expect it to land in
  the AC checklist later from memory.

## 2026-08-15 — CROC-003 — JWT helpers + auth middleware landed

- `go.mod`'s indirect/direct flags went stale a second time (`godotenv`
  in CROC-001, `golang-jwt`/`uuid` here) — `go mod tidy` catches it, but
  nothing runs it automatically; addressed by adding `go mod tidy -diff`
  to `CROC-003a`'s CI scope before that ticket gets built.
- `/code-review` found two real bugs (a valid token could get wrongly
  rejected) in code inherited verbatim from `packing-list-go` under an
  explicit "no deltas" decision; left unfixed at first out of
  over-deference to that decision, until the founder pushed back.
- **Pattern**: "match the reference" from `grill-me` is a starting
  point, not a freeze against later `/code-review` findings — ported
  code gets fixed like any other finding unless there's a real reason to
  preserve behavioral parity.

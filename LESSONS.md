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

## 2026-08-16 — CROC-003a — CI checks pipeline landed

- No rework in the pipeline itself, but AC verification was partial: only
  1 of the 5 "each independently fails CI" conditions was actually
  exercised (a real `govulncheck` failure); the other four were accepted
  on precedent (`packing-list-go`'s already-proven pipeline) rather than
  deliberately triggered — a real, informed exception, not an oversight.
- Paid off immediately: the first real PR caught a genuinely stale Go
  version pin in `go.mod` (7 real stdlib CVEs) before it could sit silent
  — exactly the failure mode this ticket existed to prevent.

## 2026-08-16 — CROC-004 — Google OAuth login flow landed

- Auto Mode plus an unwritten "AI-driven ticket" cadence let ~9 pieces of
  work chain together with no review checkpoint, including a live schema
  change against the real Neon dev DB — caught only once the founder
  stopped and asked to restart; fixed by writing the missing "test, stub,
  confirm red, stop" cadence into `CLAUDE.md` for AI-driven mode
  (previously only the hand-written-mode roadmap had it explicit).
- `/code-review`'s default multi-agent effort (8 finder sub-agents) ran
  for a routine per-ticket review, stalled on one sub-agent for 10+
  minutes, and burned a large share of session budget before landing on
  any findings — fixed by pinning per-ticket reviews to `medium` effort
  in `CLAUDE.md`, reserving `high`/`ultra` for the periodic security/
  tech-debt pass.
- **Pattern**: a ticket-mode or tool-invocation default needs its
  behaviour spelled out explicitly before the first ticket that actually
  exercises it — both misses here were "assumed to carry over from
  somewhere else" gaps, not new mistakes each time.

## 2026-08-16 — CROC-005 — Email/password auth shipped; CodeRabbit caught a real account-takeover bug in Claude's own design reasoning, not just implementation

- TDD stop-discipline (confirm red, then wait for go-ahead) was skipped
  twice mid-ticket despite `CROC-004`'s lesson above already naming this
  exact failure — caught by the founder both times, not self-caught.
- `Register`'s abandoned-signup retry path overwrote a stranger's
  password before the grill-time "no account-takeover risk" reasoning
  was actually checked against what happens when the legitimate owner
  (not the attacker) completes confirmation — CodeRabbit's review caught
  it, not the grill or Claude's own review.
- **Pattern**: per-ticket (and periodic) code review moves to CodeRabbit
  via PR, not `/code-review` — Claude's own `medium`-effort review burned
  roughly a fifth of a session's usage in about three minutes for one
  ticket's diff. `grill-me` now requires the reasoning alongside every
  recommendation, not just the recommendation.
- The founder asked at close-out whether `auth_handler_test.go`'s size
  was normal — the question went unanswered/unrecorded here, so it
  resurfaced unresolved at `CROC-006`'s close-out. Checked against
  `packing-list-go` then: its `auth_handler_test.go` is 1059 lines for a
  291-line handler, larger than this project's equivalent at the time of
  asking — auth is the widest-branching handler (OAuth + password +
  email confirm + token issuance) in both codebases, and both already
  split one test file per handler, so there's nothing left to
  consolidate it with. Answered as "normal, checked against precedent,"
  not left open. **Pattern**: a question asked at close-out needs an
  answer captured here in the same pass, even a quick one — otherwise it
  re-litigates from scratch next time with no memory of already being
  raised.

## 2026-08-16 — CROC-006 — Email/password login shipped; CodeRabbit caught a session-ordering bug my own fix-round got backwards

- The hand-written-mode roadmap wrongly stated "tests after" a second
  time, contradicting `CLAUDE.md`'s own already-stated rule — fixed the
  wording at the source instead of relying on prose alone to hold.
- My own reorder of `Login`'s side effects protected against the wrong
  failure mode (a stale `last_login_at`) instead of the worse one (a
  live session issued despite a reported failure) — CodeRabbit caught
  it, not the grill or my own review.
- **Pattern**: when ordering non-transactional side effects around a
  possible failure, the one that grants access goes last — protect
  against a working session existing when the response says it failed,
  not against stale metadata.

## 2026-08-17 — CROC-007 — Forgot/reset password shipped; CodeRabbit caught two lifecycle bugs in my own transaction fix, not just the original diff

- Round 1 correctly rejected a disclosure finding that contradicted an
  already-deliberate decision (checked against Register/ResendConfirmation/
  Login precedent), but building the transaction fix for a real concurrency
  gap introduced two new bugs — `require.NoError` inside a test goroutine
  and a missing deferred rollback on panic — both caught by CodeRabbit's
  second pass, not self-caught.
- **Pattern**: CodeRabbit is the accepted real gate for this process, not a
  formality — third ticket running where it catches something a clean
  first-pass implementation missed. No mechanization needed; founder
  confirmed this is the intended shape of the process.

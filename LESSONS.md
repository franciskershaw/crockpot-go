# Lessons

Running retro log for this repo. One entry per ticket close-out: what
caused rework (if anything), what pattern should become a standing rule,
and whether this file or the project's own `CLAUDE.md` needed a new line
as a result. Reviewed at the start of every new ticket's `grill-me` and at
project kickoff.

## 2026-08-31 — CROC-024 — MongoDB→Postgres dev migration (one-off `cmd/migrate-data`)

- No implementation rework across 6 pieces. Grill decisions 4/6/7 were
  reshaped when the Compass export arrived mid-grill (0→3 users once the
  data showed a ghost seed-import account; hand-edit-in-Mongo →
  hard-coded `allowedUnitAdditions` table after founder push-back). Dry
  run found 620 ingredients using the old blank `{name:""}` Unit doc as
  "unitless" → mapped to NULL; the CROC-011 lesson had named that exact
  row but grill discovery didn't check the Unit collection for it.
- `/code-review` found the reconciliation check was tautological
  (`skipped := source − destination`, so `source − skipped = destination`
  always held) and recipe category links weren't de-duped though
  ingredients were. Both fixed; second dev run clean, `in-db` counts now
  from a real post-commit `count(*)`.
- Migration ran 0-skipped first attempt; 100% reference-name match.
- **Pattern**: a grill whose decisions turn on the shape of external
  data is provisional until the data is in hand — pull the export before
  writing the handoff, not after.

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

## 2026-08-17 — CROC-008 — Refresh + logout shipped; CodeRabbit's fourth real catch in a row, this one a genuine TOCTOU race in the core rotation mechanism

- No rework in the TDD layers themselves (repository and handler both
  red→green clean). CodeRabbit's post-review pass found 3 real issues: a
  stale doc claim, `Logout` silently swallowing a `RevokeFamily`
  failure, and an unconditional `RotateFamily` `UPDATE` racing against a
  concurrent rotation — invisible to mocked handler tests by
  construction, only provable once the check moved into the SQL `WHERE`
  clause and got a real-DB repository test.

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

## 2026-08-26 — CROC-009 — GET /me shipped; caught a masked-500 bug in CROC-008 and an import cycle before either shipped

- `RefreshToken`'s `ErrUserNotFound` handling was masking a real 401 as
  a generic 500 (never deliberate, just a leftover catch-all branch) —
  caught while grilling `/me`'s own missing-user case, fixed in both
  places since same root cause. Separately, `testutil.AuthHeader`
  mirroring `packing-list-go` would have been a real import cycle in
  this repo specifically — caught by running `go vet` against a scratch
  file before committing to the plan, not by trusting the borrowed
  precedent.
- The `.http` coverage plan (append to `auth.http`, matching
  `packing-list-go`) didn't survive actual use — REST Client scopes
  variables/cookies per file, so a protected-route `.http` file always
  needs its own `Login` regardless of which file it lives in. Reversed
  to a standalone `me.http` post-implementation.
- **Pattern**: a borrowed precedent (`packing-list-go`, or a decision
  recorded at grill time) still needs checking against this repo's own
  structure and against actually using the thing, not just against the
  reference project — both already-standing rules (verify claims;
  `packing-list-go` is a starting point, not a mandate) did their job
  here, no new rule needed.

## 2026-08-26 — CROC-009a — CORS middleware shipped; clean port, but branch ordering caused one conflict three times

- Clean TDD port (30-line `cors.go`, 3 tests, one `main.go` line);
  CodeRabbit's only finding (`Vary: Origin`) was preventive, not a bug.
  Mode flipped hand-written → AI-driven mid-ticket with no friction.
- The CROC-009 / CROC-009a spec bullets conflicted three separate times:
  the CROC-009 branch's work was locally merged into this ticket branch
  before its own PR #8 landed in `main`, so `main` later re-merged the
  same commits via a different merge commit — bloating this PR's diff to
  14 files until `main` was merged back in.
- **Pattern**: don't local-merge an unmerged feature branch into your
  ticket branch — wait for its PR to hit `main`, then branch or merge
  from `main`.

## 2026-08-30 — CROC-010 — Item categories CRUD shipped; first resource-handler template, one real live-DB discovery

- No rework in the TDD layers (RequireRole, repository, handler all
  red→green clean). Real discovery: `ON DELETE RESTRICT` raises Postgres
  `23001` (restrict_violation), not `23503` as the handoff assumed —
  caught live against the real Neon DB (PG 18.6; PG 15+ split RESTRICT
  into its own code), code fixed, not the test.
- CodeRabbit's free trial ended mid-ticket; review moved back to
  `/code-review`. Its `low` pass's one finding was a false positive —
  flagged `23001` as wrong without live-DB access, defaulting to the
  textbook `23503`; `medium`, asked for since this ticket is the
  10-ticket template, independently re-derived the correct answer.
- `/code-review` with no base given scoped itself to the latest commit
  only, not this multi-commit ticket's full diff — self-caught, fixed by
  passing `main` explicitly.
- **Pattern**: a code-review finding about DB/runtime-specific behavior
  needs checking against the real dependency same as a design claim —
  don't downgrade live-verified evidence because a reviewer without DB
  access contradicts it.

## 2026-08-30 — CROC-011 — Units CRUD shipped; clean template application, one real tooling snag

- No rework in the TDD layers — the `23001` catch from `CROC-010`
  applied correctly first-attempt, no rediscovery needed. One real
  data-quality catch: the MongoDB export had a blank
  `{name:"",abbreviation:""}` row, excluded at grill time before the
  seed migration was written.
- `go run main.go` fails to compile — this package's `main` is split
  across `main.go` and `lifecycle.go`; `go run <file>.go` only builds
  the named file. Use `go run .` to start the server ad-hoc.
- `/code-review medium`'s one finding (the `item_allowed_units` `CASCADE`
  gap) was factually correct but not new — already a deliberate,
  recorded grill decision; the review just exposed that the code didn't
  self-document why. Fixed with a one-line comment, not a behavior
  change.
- **Pattern**: a review agent has no visibility into the handoff doc — a
  finding that restates an already-made, documented decision isn't a
  bug, but is a signal the code should say why inline, not just in the
  doc.

## 2026-08-30 — CROC-012 — Items CRUD + first many-to-many join table shipped

- No implementation rework — three bugs caught along the way were all in
  my own test code (a nil-map panic, an unsorted-comparison assertion, a
  test not accounting for `Create`'s non-atomic-without-tx behavior), not
  the repository/handler. The `medium` review's one finding (missing
  `Update`-side rollback coverage) was real this time, fixed and
  verified.
- Founder's mid-ticket pushback on magic SQLSTATE strings led to a real
  simplification: `pgerrcode` named constants + a shared
  `pgConstraintError` helper, collapsing three near-duplicate functions
  across all three reference-data repositories — including already-
  shipped `CROC-010`/`CROC-011` code.
- Two verification-hygiene misses, both self-caught: a stale server from
  an earlier session's `pkill` was still bound to the port, causing false
  404s until checked via `lsof -ti:PORT`; leaked test rows from an
  earlier debugging cycle sat in the shared dev DB until swept.
- **Pattern**: verifying against a long-lived shared resource (a
  background server, a dev DB) needs checking the resource's actual
  state — port binding, a `LIKE 'repo-test-%'` sweep — not trusting an
  earlier cleanup step succeeded.

## 2026-08-30 — First tech-debt pass, whole codebase (11 tickets in)

- First-ever periodic pass, requested after CROC-011 rather than waiting
  for a fixed cadence. Covered `internal/`, `db/`, `config/`, `main.go`,
  `lifecycle.go`, `.githooks/`. 9 findings, 7 tickets (`CROC-031`–
  `CROC-037`), full detail `docs/findings/2026-08-30-tech-debt.md`. One
  real correctness bug found (`CROC-031`, latent — a repository silently
  ignoring an active transaction), not yet exploited since nothing calls
  it inside `WithinTx` today.
- Reconciled three already-decided, already-documented tradeoffs
  (`item_allowed_units` CASCADE, no soft deletes, repo-interfaces-in-
  -handler) without re-filing them — the `CLAUDE.md`/`LESSONS.md` read
  before filing anything did its job.
- **Pattern**: a decision "flagged... out of scope" in a closed ticket's
  handoff doc (`auth_handler.go`'s helper retrofit, flagged at `CROC-010`)
  can quietly never become a real backlog item — a tech-debt pass is
  where that gets caught; grep the backlog for a flagged item's own
  wording before assuming it's tracked.

## 2026-08-30 — CROC-013 — Recipe categories CRUD shipped; one real schema decision, one self-caught process slip

- No implementation rework — repo and handler TDD both passed green
  first attempt, including the `23001` RESTRICT SQLSTATE reused without
  rediscovery. One process slip: skipped the stub/red step on the
  repository piece, went straight to a full implementation — self-caught
  before any test ran, backed out to a proper stub, redid it test-first.
- Real schema divergence caught before it shipped: unlike
  `item_categories`/`units`, `recipe_categories_recipes`'s FK was
  `ON DELETE CASCADE` (silent unlink) — confirmed live via
  `pg_constraint`, not assumed, then flipped to `RESTRICT`.
- **Pattern**: the AI-driven stub/red/stop gate applies to every piece
  of a ticket, not just the first one of a session — template-reuse
  familiarity is not a reason to skip it.

## 2026-08-31 — CROC-014 — Recipe creation shipped; clean TDD, grill missed three things caught mid-build

- No rework in any TDD layer (repo + handler both red→green first try).
  But the founder's build-time questions each landed a real fix: a
  `recipe_ingredients.position` column (submit order — folded in, not
  deferred); signed vs. unsigned Cloudinary upload (grill covered "API
  stores two strings", not how the browser safely gets them → `CROC-040`);
  handler structure primed to bloat across CROC-015–018 → split into
  `recipe_requests.go`, filed `CROC-041`.
- **Pattern**: a first-ticket-of-an-epic grill must probe child-table
  ordering, the client side of any external integration, and any file
  about to be copied 4+ times.
- **Pattern**: after code is green the next step is `/code-review medium
  main`, then `/close-out` — never jump straight to close-out.

## 2026-08-31 — CROC-041 — API error-response shape unified onto shared handler helpers

- No rework. All 4 pieces went refactor → handler/middleware suite green
  (zero test edits) → stop, first attempt; `/code-review medium` found
  nothing. Scope grew at grill by design — absorbed tech-debt findings
  6 + 8, leaving CROC-036 as finding 7 only. One unplanned helper
  (`serverError`) for two `auth_handler.go` sites whose error was
  already wrapped, where `internalError` would have doubled the log prefix.
- **Pattern**: when the handler suite already asserts both status and
  `error` code for every path, a package-wide error-shape refactor is
  safe to run behaviour-preserving with no new tests — lean on the
  existing assertions instead of re-deriving coverage.

## 2026-08-31 — CROC-032 — FK indexes (all 10) + a schema-assertion test

- No rework. Clean ticket. Test RED confirmed all 10 columns genuinely
  unindexed (validated the tech-debt audit); one migration took it
  green; `migrate down 1`/`up` round-trip clean; review clean.
- Grilled and built ahead of CROC-015 as sequencing hygiene, not a
  technical unblock — index the tables, then build the read-heavy
  feature on them.
- **Pattern**: assert a schema invariant by its property, not its
  name — `TestSchemaFKColumnsAreIndexed` checks each FK column *leads
  some index* via `pg_index.indkey[0]`, catching a valid-SQL-wrong-column
  migration typo without pinning index names.

## 2026-08-31 — CROC-015 — Recipe read layer (GET /recipes + /:id); relevance ranking split to CROC-042

- No implementation rework. The grill's main correction was the
  founder's: keep the old app's relevance ranking (the "what can I cook
  from these ingredients?" use case is core, esp. for anon users) —
  split into CROC-042 with its own grill + a likely `recipe_categories`
  schema change. CROC-015 shipped endpoints/DTOs/visibility/filters/
  pagination; its filter vocabulary is the candidate net ranking will
  order.
- `/code-review` caught an `int32(minTime)` overflow that silently
  dropped the time filter — and it was inconsistent with the `page`
  overflow clamp added earlier in the same piece. `maxInt` also
  reimplemented the 1.21 builtin. Both fixed.
- **Pattern**: when you add a defensive clamp/guard for one input,
  apply it to every input of the same kind in the same pass — a
  half-applied guard reads as deliberate and hides the gap.

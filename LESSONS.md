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

## 2026-09-02 — CROC-018 — Favourites (POST/DELETE /recipes/:id/favourite, GET /recipes/favourites)

- Hand-written mode's practice intent landed thin: repo/handler bodies
  mostly ended up Claude-written after struggling first attempts that
  each copied the wrong neighbouring method's shape (`AddFavourite`
  copied `Get`/`GetByID`'s read-and-return pattern instead of the
  check-then-write one). `CLAUDE.md`'s stale "Claude writes no code, not
  even TDD stubs" line was corrected mid-ticket to match actual
  practice.
- `/code-review` caught one real duplication (category hydration
  repeated in `List`/`ListFavourites`, now a shared
  `hydrateCardCategories` helper) and one false positive already
  covered by the ticket's own documented non-goal (decision 5).
- **Pattern**: GUI git clients invoke hooks with a bare PATH (no
  Homebrew bin dir) — `.githooks/pre-commit` now exports `PATH`
  explicitly rather than trusting the caller's environment.

## 2026-09-04 — CROC-043 — Recipe cooking-time bounds (`GET /recipes/time-range`)

- No implementation rework — sqlc query, repository, and handler each
  went red→green clean, one piece at a time.
- `/code-review medium main` found two things: `RecipeTimeRange`
  (`internal/models/recipe.go`) shipped with no `json` tags, the only
  model in that file without them — forced the handler into a hand-rolled
  `gin.H{...}` instead of the `c.JSON(status, model)` every other
  single-object handler uses. Fixed immediately (tags added, handler
  simplified) — real, cheap, no rework.
- Also flagged: each piece's red→green ran as one continuous motion
  after the founder's go-ahead, with no second stop *between* confirming
  red and implementing. This is the same gap `CROC-004` and `CROC-005`
  already named — third occurrence now. Per this project's own "same
  feedback three times, mechanise or drop" rule: the founder's
  "go ahead"/"go go go" replies are being read (correctly, by working
  practice) as authorizing the whole next piece, not as permission to
  fold the red-stage stop into it — `CLAUDE.md`'s "AI-driven tickets"
  wording should say that explicitly rather than get re-flagged a fourth
  time. Not changed this ticket — founder asked to close out with
  minimal fuss; revisit at the next relevant grill.
- **Pattern**: a model with no `json` tags is a signal, not just a style
  gap — every sibling in the same file having them makes the omission
  the kind of thing a diff review should always catch, and did.

## 2026-09-06 — CROC-031, 033–037 — Second tech-debt pass shipped clean; `/code-review` caught a self-inflicted regression, not an implementation bug

- All six tickets went red→green clean on the first attempt — no
  implementation rework. Mid-batch, a process gap surfaced: two tickets
  were implemented back-to-back before the founder had actually
  committed the first, despite "stop at reasonable commit boundaries" —
  corrected explicitly, followed correctly for the remaining four.
- `/code-review medium main` (run once across the whole six-ticket
  batch, not per-ticket) found the one real bug of this batch: writing
  CROC-037's `db_test.go`, `Write` was used on a file that already
  existed (with `TestInitDB_EmptyDatabaseURL` in it) and silently
  clobbered that test instead of appending to it — not self-caught.
  Also flagged (and fixed): `runTokenSweeper`'s own orchestration logic
  had no test, only the repository methods it calls did; two
  structurally identical sweep interfaces should have been one.
- The review itself hit a real cost problem, unrelated to the code:
  launched at 68% session usage, its 9-agent parallel fan-out (shared
  usage pool across all sub-agents) tipped the session over its limit
  within seconds, failing with zero findings delivered. An identical
  re-run after a full limit reset cost only ~5% — likely because
  retry/backoff thrashing near an already-strained limit inflates real
  cost far above the same work run with headroom (not fully verified,
  no per-agent telemetry available). Reported as product feedback; no
  policy change made this ticket (founder deferred the decision).
- **Pattern**: before adding a new test file, check whether it already
  exists and read it first — don't assume a file is new just because
  the current ticket's own tests are the only ones you're thinking
  about. And: a multi-agent review's cost is unpredictable in advance —
  prefer running one with real usage headroom (well under the limit),
  not saved up for the end of a session.

## 2026-09-06 — CROC-019 — Menu read/upsert-entry shipped; stop-discipline skipped a 4th time, same failure CROC-004/005/043 already named

- No implementation rework in any piece — every red confirmed for the
  right reason, every green passed first attempt. The one process miss:
  piece 3 (repository) went red → full implementation as one continuous
  motion with no stop for review in between, despite `CLAUDE.md`'s
  explicit AI-driven cadence; only corrected from piece 4 onward, after
  the founder flagged it. `CROC-043`'s lesson had already said to
  revisit this wording at the next relevant grill — this was that grill,
  and it wasn't raised proactively.
- `/code-review medium main` found a stale master-spec line ("not yet
  built" on a fully-shipped ticket) and a minor unused-field over-copy —
  both fixed clean, one pass.
- **Pattern**: after confirming a piece's tests are red, stop and report
  before writing any real implementation — a "go ahead" on the prior
  piece is not standing authorization to skip this checkpoint on the
  next one.

## 2026-09-06 — CROC-042 — Recipe relevance ranking shipped; code-review fixes applied before the founder saw the findings

- Piece 2 (ingredient coverage) skipped the red-stage checkpoint again —
  5th occurrence of the CROC-004/005/043/019 pattern, self-caught this
  time before being flagged. Two real SQL bugs also surfaced during the
  build itself (not review): Postgres won't resolve a same-level
  SELECT-list alias inside an ORDER BY CASE, and sqlc collapsed a
  mixed-type CASE to interface{} until every branch was cast to float8.
- After `/code-review medium main` returned 5 findings, 4 were fixed
  immediately without showing the founder what they were first — the
  founder wants findings surfaced before any fix, even when approval is
  likely.
- **Pattern**: after any review/analysis step returns findings, present
  them in plain language and wait for go-ahead before fixing anything.

## 2026-09-08 — CROC-016 — Recipe update/delete shipped; `.http` file editing stalled badly mid-ticket

- Piece 2 briefly went past the red-stage stop into a real refactor
  before review — caught immediately, piece restarted, remaining pieces
  held the stop-at-red discipline correctly.
- Piece 5's `.http` file edit took far longer than warranted: repeated
  re-reads, and one large insert got silently reverted on disk between
  tool calls (likely an editor autosave from an open buffer), which took
  another read-and-redo cycle to catch and fix — founder flagged the
  wasted time and tokens directly.
- Branch-review (2-agent bugs/security + a manual quality pass, run
  instead of a full `/code-review medium main`) caught one real,
  low-severity bug: `Delete` skipped the transaction wrapper `Update`
  uses for the same check-then-act pattern. Fixed and verified.
- **Pattern**: before a large edit to a file that might be open
  elsewhere (`.http` regression files especially — these live in an
  editor with the REST Client extension open more often than most repo
  files), re-read it immediately beforehand rather than trusting an
  earlier read from several tool calls ago.

## 2026-09-18 — CROC-021 — Shopping list generate/regenerate shipped; one review finding was a false positive, caught by checking outside the diff

- No rework from a locked test or a skipped stop-and-review checkpoint.
  One self-caught near-miss: a red-stage test would have passed against
  its own stub by coincidence, tightened before reporting.
- `branch-review`'s bugs agent flagged a medium-confidence concurrency
  race in `Regenerate`'s insert; checking `tx.go` (outside the diff it
  saw) showed `GetOrCreateShoppingList`'s upsert already serializes
  same-user regenerations via Postgres's `ON CONFLICT DO UPDATE` row
  lock — documented with a comment instead of a redundant fix.
- **Pattern**: a diff-scoped review agent can't see transaction-scoping
  code that lives outside the diff — verify a concurrency finding
  against the actual transaction boundary before accepting it as real.

## 2026-09-18 — Second whole-codebase tech-debt pass

- Covered `internal/`, `db/migrations/`, `config/`, `main.go`,
  `lifecycle.go` (`cmd/migrate-data` excluded, one-time script, not part
  of the running service). All 9 first-pass findings (`CROC-031`–`037`,
  `041`) confirmed still holding on re-check, nothing regressed. 4 new
  findings, 3 tickets (`CROC-046`–`048`), full detail
  `docs/findings/2026-09-18-tech-debt.md`.
- The one non-trivial find: `CROC-041`'s error-shape retrofit covered
  `handler` and `middleware/rate_limit.go` but missed
  `middleware/auth.go` (a different package) — its three 401 bodies are
  still sentence-style, and `auth_test.go` only asserts the status code,
  never the body, so nothing caught the gap. Also found: `recipe.go`'s
  `Create`/`Update` duplicate a ~30-line ingredient/category-write loop
  pair unreconciled since `CROC-016`, and `AddFavourite` is the one
  check-then-write on `RecipeHandler` not wrapped in `WithinTx` (same
  shape as `CROC-031`, currently benign for the same "not yet exploited"
  reason).
- **Pattern**: an error-shape (or any cross-cutting) retrofit ticket
  should be checked against every package that emits that response type,
  not just the one the ticket's own diff touched — `middleware` isn't
  `handler`, and a same-directory grep alone would have missed it.

## 2026-09-18 — CROC-046 — `auth.go`'s 401 bodies collapsed to the shared `unauthorized` code

- No rework. Clean ticket. Test went red for the right reason (exact
  body mismatch, no panic) on the first attempt; three one-line edits in
  `auth.go` turned it green on the first attempt.
- Grill checked the frontend's actual retry logic (`client.ts:61`,
  status-only) and this codebase's own 13/17-call-site convention before
  picking "collapse to one code" over inventing three new ones —
  confirmed rather than assumed. `code-review` skipped by choice, size
  didn't warrant it.

## 2026-09-18 — CROC-047, CROC-048 — Tech-debt cleanups batched, no rework

- No rework. Both fully specified in the findings doc (no open decision),
  so grill was skipped for a one-message scope recap instead; run
  straight through per the founder's choice rather than stopping at each
  red/green boundary. All existing tests (handler mocks + real-DB
  recipe/favourite/menu suites) green unmodified — pure refactors.
- Close-out itself scaled down to just marking Done + this entry, at the
  founder's request — no retro Q&A for a batch this small and clean.

## 2026-09-18 — CROC-022, CROC-049 — Shopping-list manual edits + clear-menu gap shipped; two grill corrections, one real concurrency bug caught by branch-review

- Old-app precedent got used as justification twice during CROC-022's
  grill (manual-add shape, then quantity-editing scope) before being
  re-derived from this project's own schema and constraints — corrected
  each time, but reasoning from source first would've been faster.
- Branch-review (bugs + security agents, independently, plus the
  session's own read) caught a real bug: `AddManualItem`'s
  find-then-write wasn't wrapped in `WithinTx`, unlike sibling
  `DeleteItem` — fixed and verified with a real concurrent-goroutines
  test under `-race`, not just reasoned about.
- **Pattern**: before asserting what a reference app does or doesn't do,
  re-check files already read earlier in the same session — a
  data-layer function seen once and forgotten led to a wrong claim
  ("this sounds like a new capability") the founder had to correct
  directly.

## 2026-09-19 — CROC-020 — Menu history (diary + frozen baseline) shipped; a "keep in sync" comment had silently drifted

- The migrator's `TRUNCATE` list had been broken since CROC-021 behind a
  "keep in sync with 000001" comment; only writing a guard test first
  exposed it. Branch-review found the new history path skipped the
  zero-date guard recipes already get (`fallbackTime`), and the data-shape
  grill needed a concrete scoreboard-vs-diary example before it landed.
- Slips: called a docs commit "ready" after it was committed (no `git
  status`), and a raw `grep` of `.env` printed the dev DB password.
- **Pattern**: turn "keep in sync" comments into tests; list a
  neighbour's guards before writing its sibling; `git status` before
  naming a commit boundary; read `.env` by key name, never echo a line.

## 2026-09-26 — CROC-052 — Shopping-list regenerate (full reset) shipped; build clean, grill needed two corrections

- No rework in the build. The grill asked for a decision on "dismissals" (internal CROC-021 term) unexplained, then over-designed a merge that preserved ticks/manual items when the founder's framing meant a literal full reset. Stale `/code-review` guidance in `CLAUDE.md` also went unnoticed until the founder flagged it.
- **Pattern**: translate codebase-internal terms into user-facing behaviour before asking for a decision on them; propose the literal reading of the founder's framing first, nuance only if asked.

## 2026-09-26 — CROC-053 — Manual add merges into the existing row; build surfaced a pre-existing dismissal bug

- The ticket took three founder pushbacks to reach: I framed the duplicate as a frontend display problem, then offered "two lines" and a schema change before "adding works like a quantity edit". Second literal-reading miss in one day (see CROC-052).
- Rewriting the `.http` walkthrough exposed a real bug (dismissals stored the displayed quantity, so edited rows reappeared) and a walkthrough that had never shown what it claimed. Branch review then backlogged CROC-054–056.
- **Pattern**: answer the founder's framing literally before adding nuance; when a walkthrough step claims "unrelated", check the fixture actually is.

## 2026-09-27 — CROC-057 — Menu cards carry isFavourite; clean

- No rework. The ticket named the cause and the fix, and both checked out against source, so no grill was needed. The favourite lookup moved into a shared `markFavourites` helper instead of being copied.

## 2026-09-28 — CROC-060 — Own-recipes list newest first; clean

- No rework. The ticket named both the cause and the fix, and both checked out against source. Branch-review only turned up two wording nits.
- **Pattern**: when the red state is a random shuffle, use enough rows that it can't match the expected order by chance (5 rows, 1 in 120).

## 2026-10-02 — CROC-061 — Non-ingredient item categories; clean

- No rework. Branch-review caught one fragile test: it asserted over every row in the shared DB, so a leftover fixture would fail it forever. Fixed by skipping `repo-test-` rows, as the neighbouring prefix-scoped test already does.

## 2026-10-02 — CROC-058 — Recipe delete resyncs shopping lists; clean build, one doc slip

- No rework in code. The lock test was made deterministic (hold one tx, poll `pg_stat_activity` for a lock wait) and seen failing 5/5 before `FOR UPDATE`. Branch-review caught the grill block written under the next epic heading: the recording script's "next ticket" boundary crossed a `###`.
- **Pattern**: when editing the spec by script, bound an entry by the next bullet *or heading*, and eyeball the diff's placement.

## 2026-10-02 — CROC-062 — Approved recipes locked to admins, edits resync lists; clean

- No rework in code. The grill took three reframes (keep owner delete → un-hide pending for holders → remove on reset) before the founder's actual rule — approved recipes belong to the community — made all three moot. Folding the follow-up (edit resync) into this ticket saved a grill.
- **Pattern**: when a product question keeps spawning edge cases, ask what the founder thinks the object *is* (whose it is, once published) before designing around each case.

## 2026-10-02 — CROC-040 — Recipe photos via proxied upload; the grill flipped the architecture mid-way

- No rework inside pieces. Design rework: signed direct upload became a proxied save mid-grill, and decision 8 (URL parsing) survived the flip with no caller until piece 3. Review found a misconfigured dev folder could destroy prod photos (now a config guard); the frontend session found `Retry-After` hidden by CORS. The first curl run hit an older build already on :8080.
- **Pattern**: when a grill answer changes the architecture, re-check every earlier decision against it and drop those with no caller.
- **Pattern**: a response header the frontend reads is part of the contract — check CORS exposes it.
- **Pattern**: check the port is free (`lsof -iTCP:<port> -sTCP:LISTEN`) before starting a local server; another session may be running an older build.

## 2026-10-02 — Third whole-codebase tech-debt pass

- Covered `internal/`, `db/migrations/`, `config/`, `main.go`, `lifecycle.go` (`cmd/migrate-data` excluded). Auth/token code was unchanged since the last pass, so it got a spot-check only. 7 findings across 4 tickets (`CROC-063`–`066`), full detail in `docs/findings/2026-10-02-tech-debt.md`. The recipe-cap TOCTOU was reconciled as already accepted at CROC-014, not re-filed.
- The real find: shopping-list unit merging looks base units up by admin-editable *name*, so a rename silently corrupts every list ×1000. Otherwise: unbounded quantities overflow `NUMERIC(10,2)` into 500s, and the visibility predicate is hand-copied in 4 queries.
- **Pattern**: a SQL lookup that keys on a mutable display field (name) instead of the structural columns a migration set is a latent bug. Grep queries for `name = '` literals.

## 2026-10-03 — CROC-045 — Gzip API responses; clean

- No rework. Reading the library source before placing it showed that `gin-contrib/gzip` deletes the whole `Vary` header on error and empty responses. Registering it after CORS keeps `Vary: Origin` on preflights; the founder accepted the loss on errors. `/items` dropped 87%. Branch-review found nothing.
- **Pattern**: before adding header-rewriting middleware, read the source and probe it against the existing chain's headers. A middleware can strip a header another one set.

## 2026-10-03 — CROC-067 — Retry-After on resend_too_soon; clean

- No rework. Branch-review found the cooldown seconds were truncated, so the header could say `Retry-After: 0` with time still left. Now rounded up, with a test for a partial second; `recipe_images.go` already floored its value at 1.
- **Pattern**: none.

## 2026-10-03 — CROC-064 — Quantity bounds and overflow → 400; clean

- No rework in code. The grill corrected two claims in the tech-debt finding before any code: its example ceiling (100,000) can't prevent the overflow it targets, since a unit's `base_factor` × the serves ratio × the menu-wide sum still passes `NUMERIC(10, 2)`; and "no client-side cap" was wrong, since `CFE-042` had added one at 999,999.99. Branch-review's one bug lead was a false positive, settled by reading the regenerate SQL outside the diff.
- **Pattern**: when a fix proposes a bound to prevent overflow, multiply the bound through every downstream scaling factor before accepting it. A bound that can't prevent the failure is input hygiene, and the error mapping is the real fix.

## 2026-10-04 — CROC-038 — Regulars and restock shipped; clean

- No rework. One handoff claim was wrong (the FK-index schema test checks a hand-kept list, not every FK) and was caught at the first piece. Branch-review's two bug leads were false positives again, settled outside the diff: the list upsert's row lock serialises restocks, and item/unit deletes map any restrict violation.
- **Pattern**: when a reviewer flags a race that an existing lock already prevents, add a deterministic lock-wait test (CROC-058's `waitForLockWait`) and prove it fails with the lock removed, so the question stays answered.

## 2026-10-04 — Fourth whole-codebase tech-debt pass

- Covered `internal/`, `db/migrations/`, `config/`, `main.go`, `lifecycle.go` (`cmd/migrate-data` excluded). Whole-codebase, but per the founder, nothing already open on the backlog was re-filed (063, 065, 066, 056, 059 and 051's "sign out everywhere"). 8 findings across 4 tickets (`CROC-068`–`071`), full detail in `docs/findings/2026-10-04-tech-debt.md`.
- The real finds: the server timeouts were never re-checked after `CROC-040` added 6 MiB photo uploads, so a slow save can commit and still lose its response (the Cloudinary client's 20s timeout is longer than the whole 15s write budget). Also, email addresses have been matched case-sensitively since `CROC-002`, which nobody had ever raised.
- **Pattern**: a ticket that makes a request much heavier (a big body, an outbound call) has to re-check the global server timeouts it now runs under. Those limits were set for small JSON requests.

## 2026-10-07 — CROC-065 — Visibility rule in one SQL function, List/Count merged; clean build, one grill correction

- No rework in code. The grill corrected two things: the finding missed that `TestListRecipes_CountMatchesListLength` already existed, and my window-count recommendation broke a CROC-015 contract (correct `total` on an over-range page) that a test locked. I caught it only after the founder had agreed. The red stub moved from `NOT approved` to `NULL` so no truth-table row passed by coincidence. A migration stub applied to the shared dev DB needs `migrate down 1` before the real body goes in.
- **Pattern**: before recommending a change that gives up a behaviour, grep handoffs, spec and tests for that behaviour first, not after agreement.

## 2026-10-08 — CROC-051 + CROC-030 — Account management shipped; the password-check race took two review rounds

- Three handoff claims were wrong (decision 6's `issueRefreshSession` sets the cookie before commit, a misnamed revoke method, two of three planned repo methods already existed), each caught at its piece. Review moved the current-password check under the row lock, and the next review flagged bcrypt holding that lock; settled as check-before-lock plus a hash re-check inside. Account delete copied recipe delete's steps although the handoff said reuse; review extracted `deleteRecipeRow`.
- **Pattern**: when a fix moves a check under a lock, list what now runs under it; keep slow work outside and re-check a cheap value inside.
- **Pattern**: when a handoff says "reuse X's internals", extract the shared function in that piece rather than copying.

## 2026-10-08 — Fifth whole-codebase tech-debt pass

- Covered `internal/`, `db/migrations/`, `config/`, `main.go`, `lifecycle.go` (`cmd/migrate-data` excluded), after `CROC-065`/`051`/`030`. 3 findings: one new ticket (`CROC-072`, test plumbing), and two that widen open tickets (`CROC-068`: photo destroys run before the response; `CROC-071`: the token tables' partial `user_id` indexes can't serve the new user-delete cascade). Full detail in `docs/findings/2026-10-08-tech-debt.md`.
- **Pattern**: when a ticket turns a rare operation into a user-facing path (here, deleting a user), re-check the indexes and timeouts every cascade and best-effort step under it now depends on.

## 2026-10-09 — CROC-059 — Menu cap of 30; clean. The Neon check "hang" was a suite timeout.

- No rework in code. The grill found that the regulars cap's single-statement shape (`INSERT … WHERE count < max`) would have silently skipped the serves-change half of the menu upsert, and that `GetOrCreateMenu`'s `DO UPDATE` already serialises menu writes, so the cap is exact. Branch-review found one test that could hang instead of failing; fixed to match its neighbours' `select`.
- The failing Neon checks weren't a hang: the test running at each timeout was a different one, 0–1s in. GitHub's US runners to Neon in `eu-west-2` took 400–600s for a suite that runs in 68s locally. The suite step was dropped; the weekly version check stays.
- **Pattern**: before copying a neighbour's conditional insert onto an upsert, check that the condition can't also suppress the update branch.
- **Pattern**: when a CI test run "hangs", check whether the test running at the timeout changes between runs and compare total durations across runs before looking for a deadlock.

## 2026-10-09 — CROC-017 — Admin approval with a stale-view guard; clean build, slips in the handoff and the reds

- No rework in behaviour. The handoff's contract table had two wrong codes (malformed id is `400` via `parseID`, recipe 404 is `not_found`), caught at piece 3. One locked red decoded the success body with the string-only decoder, so it could never go green (stop-and-flag). Review found `Approve` was the fourth copy of the write-lock prelude, the same miss as CROC-051; extracted to `lockRecipeForWrite`.

## 2026-10-09 — CROC-069 — Case-insensitive email identity and an atomic attempt cap; review caught Unicode folding

- One rework: the grill chose `strings.ToLower` and a `lower()` CHECK without asking what they fold beyond ASCII. Branch review found the Kelvin sign folds to `k`, so a lookalike address matched a real account; fixed by folding A–Z only in Go and the CHECK, editing the unreleased `000018` in place. Codes and links already went to the stored address, which kept it low.
- Red without stubs worked twice: a missing migration and an unconditional increment each failed on assertions, and skipping a stub migration avoided the shared-dev-DB `migrate down` trap.
- **Pattern**: when normalising an identifier, name the exact character set and check that the app and the database fold it identically (test a non-ASCII case), before the grill records "lowercase".

## 2026-10-09 — CROC-074 — History date fallback in migrate-data; clean

- No rework. Checking the old app's schema first showed a missing date can't come from the app, which made it a defensive guard and settled the grill in one question. The recipe precedent's `now()` fallback was deliberately not copied, because this table exists to measure recency.
- **Pattern**: before copying a fallback precedent, check what the target data is for; a default that's harmless for sorting can fake the signal an analytics table exists to hold.

## 2026-10-09 — CROC-076 — First security pass, by hand and light-touch

- Covered routes, sessions, per-user scoping, input, uploads, leakage and `govulncheck`, read by trust boundary and paired with `crockpot-react`'s pass so the cookie/CORS boundary was read from both sides. 5 findings, all low or informational, one ticket (`CROC-079`). No dynamic two-user test was run; per-user scoping rests on reading every id-keyed query. The real find: the refresh cookie was `SameSite=None` though the spec chose a same-site domain layout for `Lax`.
- **Pattern**: check cookie, CORS and redirect settings against the spec's domain decision, not just against each other; a setting chosen before the domain was fixed can quietly outlive the reason for it.

## 2026-10-10 — CROC-079 — Security hardening from CROC-076

- Clean: one red/green batch, branch review found nothing. Two notes: the findings doc's "match the body-side caps" suggestion (50) didn't fit a filter, which can sensibly ask for more ids than a recipe holds, so the cap became 100; and the first `curl` checks hit the founder's own dev server on 8080 and returned a misleading 200, so real-client checks run the fresh build on a spare port.
- **Pattern**: before trusting a real-client check, confirm the binary under test is the one answering (its log, or a port nothing else holds).

## 2026-10-10 — Pre-commit hook conflict on a partially staged file (second time)

- Committing `CROC-079`, `config/config.go` was staged with two lines missing (likely the GUI's line/hunk staging). The staged copy didn't compile, so `go vet` failed, and the hook's exit-time `git stash pop` conflicted with the partial index. Nothing was committed; recovered from the stash. The first time was `docs/specs/master-spec.md`, committing the go-live reorder after `CROC-066`.
- **Pattern**: the hook's `stash --keep-index` / `pop` assumes every file is either fully staged or untouched. Recovery steps are in CLAUDE.md's "Pre-commit hook" section. If it happens a third time, discuss mechanising it (e.g. the hook refusing up front, naming any partially staged file) rather than recovering by hand again.

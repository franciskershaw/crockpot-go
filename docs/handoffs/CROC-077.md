# CROC-077 — Production cutover

**Implementation mode: AI-driven, as a guided checklist.** The founder
does the console work (Resend, Cloudflare, GitHub, Mongo, Vercel, Neon,
Google); Claude sequences it, gives the exact setting or command, and
checks each result before the next step. The small code changes are
`crockpot-react` `CFE-062` and `CFE-044`.

## Summary

Replace the old Next.js/Mongo site at `www.crockpot.app` with
`crockpot-react` + `crockpot-go`, in one session on 2026-10-10, in one
ordered sequence. The API is already live (`CROC-075`).

## Decisions

1. **`www` is canonical**, with the apex redirecting to it (as today).
   Vercel recommends `www` as primary: an apex can't take a CNAME, so a
   `www` CNAME gives its CDN more control. Sessions are unaffected either
   way, since every form shares the registrable domain `crockpot.app`.
   `FRONTEND_URL` must be exactly `https://www.crockpot.app`. A CORS
   preflight on 2026-10-10 showed it is currently `https://crockpot.app`,
   so it changes in step 2.
2. **Backups: stay on Neon Free.** That gives a 6-hour restore window and
   one manual snapshot, taken straight after the import. Keep the Mongo
   export files on the founder's Mac as a cold copy of the pre-cutover
   data. **Revisit trigger:** upgrade to Launch (7-day restore window,
   about $5 a month) at the first sign-up that isn't Francis or Zoe.
   Rejected: Launch now (no users yet to justify the cost), and a
   home-made nightly dump (new code to maintain, and it couldn't live in
   the public repo, since it would hold emails and password hashes).
3. **The item audit moves out** to `CROC-082`, after cutover. Once
   cutover is done `migrate-data` never runs again, so editing items in
   production is safe.
4. **The Vercel switch is the takedown.** There's no separate
   offline step and no freeze. Vercel is pointed at the new code before
   the import: frontend problems show up before any data moves. The site
   shows an empty recipe list for a few minutes, which the founder
   accepts. A Google sign-in before the import is harmless, because
   `migrate-data` deletes and re-inserts the migrated users
   (`cmd/migrate-data/load.go`, `deleteMigratedUsers`).
5. **One session, in dependency order.** Resend starts first because its
   verification can take a while.
6. **The 429 check uses the login limit**, not the photo limit (20 an
   hour). Ten wrong logins in a minute prove `Retry-After` survives
   Cloudflare and nginx, which is the part that's new in production. The
   upload-specific message is covered by `saveErrors.test.ts`. The
   limit is per IP and the founder shares Claude's IP, so their own
   sign-ins wait about a minute afterwards.

## Sequence and checks

| # | Step | Check before moving on |
| --- | --- | --- |
| 1 | Resend: add `crockpot.app`, add its DNS records in Cloudflare (no clash with Email Routing's `contact@`). `EMAIL_FROM` must use that domain | Records added; verification finishes in the background (step 10) |
| 2 | GitHub `production` secret `FRONTEND_URL` → `https://www.crockpot.app`, re-run the deploy | Preflight `OPTIONS https://api.crockpot.app/auth/refresh` with `Origin: https://www.crockpot.app` returns `access-control-allow-origin: https://www.crockpot.app` |
| 3 | `crockpot-react` `CFE-062` (privacy page: DigitalOcean as the API host, "Last updated" date, IP wording against nginx and Vercel log retention) and `CFE-044` (security headers in `vercel.json`); committed and merged | Suite, `tsc`, build green |
| 4 | Fresh Compass export of all 8 collections; keep the files | 8 files; `migrate-data` dry run (no `--yes`) reads them and prints counts |
| 5 | `CFE-066`: re-point the existing Vercel project at `crockpot-react` (Vite preset, `VITE_API_URL=https://api.crockpot.app`). The old site goes down | `www.crockpot.app` serves the new app; the apex 308s to it |
| 6 | `MIGRATE_ALLOW=prod DATABASE_URL=<prod> go run ./cmd/migrate-data --source <dir> --allow-prod`, then the same with `--yes` | Dry run names the **production** host (founder reads it out); the real run's counts match the dry run's; recipes show on the site |
| 7 | Manual Neon snapshot of `production` | Listed in the Neon console |
| 8 | Google (`crockpot-api-final`): Branding (name, support email, homepage `https://www.crockpot.app`, privacy `https://www.crockpot.app/privacy`, authorized domain `crockpot.app`, no logo), then Publish | Founder signs in with Google on the live site |
| 9 | Live checks | `curl -sI` shows the `CFE-044` headers and the browser console has no CSP violations while using the site; every `SHOWCASE_POOL` image URL returns 200; one phone photo upload; 10 bad logins return a 429 with `Retry-After`; Francis and Zoe each see their own menu and favourites |
| 10 | Resend verified | A password-reset email reaches the founder's inbox |

The secrets stay in the founder's shell. Claude never prints them.

## Rollback

- Before step 6: Vercel Instant Rollback to the old site's last
  production deployment. Mongo is untouched.
- During step 6: `migrate-data` runs in one transaction, so a failure
  leaves nothing behind. Fix it and re-run.
- After step 6: rollback to the old site still works, since Mongo is
  untouched until `CROC-078`. This holds until new users have data only
  in Postgres.
- If Resend isn't verified by step 10: go live anyway. Google sign-in
  works; email sign-up and reset wait for verification.

## Acceptance criteria

- [ ] `FRONTEND_URL` is `https://www.crockpot.app` (preflight proves it)
- [ ] `CFE-062` and `CFE-044` merged before the Vercel switch
- [ ] `www.crockpot.app` serves `crockpot-react`; the apex redirects to it
- [ ] The production import's counts match its dry run
- [ ] Neon snapshot taken after the import; export files kept
- [ ] Google consent screen published; Google sign-in works on the live site
- [ ] Headers present, no CSP violations, showcase images return 200, a
      phone upload works, a 429 carries `Retry-After`
- [ ] Francis and Zoe see their migrated menus and favourites
- [ ] A password-reset email arrives (or Resend is still pending, noted)

## Non-goals

- The item audit (`CROC-082`).
- Retiring Mongo, the old repo, the old Google project and the droplet
  tidy-up (`CROC-078`).
- A paid Neon plan (see the trigger in decision 2).
- A web manifest or Open Graph cards.

## Verification modes

- **Service boundary:** each step's check above is a real request
  against the real dependency: preflight `curl`, the site, the import
  report, Google sign-in, Resend email.
- **Limits:** the 429 check through the real proxy chain (decision 6).
- **Interactive (founder):** both accounts' sign-in and data, one phone
  upload, reading the privacy page.

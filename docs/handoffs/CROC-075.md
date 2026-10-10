# CROC-075 — Deploy pipeline

Build and deploy `crockpot-api` to the shared DigitalOcean droplet behind
nginx and Cloudflare, from `packing-list-go`'s pipeline (`Dockerfile`,
`.dockerignore`, `nginx/packing-list-api.conf`, the `build`/`deploy` jobs
in `.github/workflows/ci.yml`; `docs/handoffs/PACK-038.md`), with the
fixes below. AI-driven. Grilled 2026-10-10.

## Context

Merging this to `main` is the first production deploy: `build` runs on a
push to `main` after `checks`. So the accounts and config the deploy reads
are this ticket's prerequisites, done before the merge (moved here from
`CROC-077`). The current site (`crockpot.app` on Vercel) is unaffected:
only an `api` record is added. The founder is fine with the old site going
down during `CROC-077` if Cloudflare/Vercel need rebuilding.

No Go code changes. The binary's only runtime files are embedded
(`db/db.go:20` migrations, `internal/email/resend.go:18` templates);
`distroless/static` carries the CA certificates for Neon, Resend,
Cloudinary and Google.

## Decisions

1. **Prerequisites move from `CROC-077` into this ticket**, in the order
   under "Prerequisites". `CROC-077` keeps what only matters for real
   users: Google consent screen "In production", Resend domain
   verification, the item audit, the data sequence, the Vercel/apex work.
2. **Real client IP, two trusted hops.** nginx trusts Cloudflare:
   `set_real_ip_from` every Cloudflare IPv4 and IPv6 range,
   `real_ip_header CF-Connecting-IP`. Gin trusts nginx: the container's
   peer is the Docker bridge gateway, not `127.0.0.1`, so
   `TRUSTED_PROXIES` is that gateway's exact IP (read on the droplet;
   expected `172.17.0.1`). `127.0.0.1` would make Gin ignore the
   forwarded headers and key every rate limit on the gateway. Not the
   whole `172.16.0.0/12`: other apps' containers on the droplet could then
   spoof `X-Forwarded-For`. nginx sets `X-Forwarded-For $remote_addr` and
   `X-Real-IP $remote_addr` (overwrite, not `$proxy_add_x_forwarded_for`),
   so no client-supplied value reaches the app.
3. **Cloudflare ranges hardcoded** in `nginx/crockpot-api.conf` from
   `https://www.cloudflare.com/ips/`, with a one-line comment naming it.
   Not fetched at deploy: a failed fetch would silently trust nobody. A
   range Cloudflare adds later only coarsens rate limits for traffic
   through it until the file is updated.
4. **Publish on loopback only**: `-p 127.0.0.1:5600:5600` in every
   `docker run`. The precedent's `-p 5400:5400` exposes the app over plain
   HTTP on the droplet's public IP, bypassing TLS, Cloudflare and nginx;
   Docker's iptables rules bypass `ufw`. The DO cloud firewall
   (`droplet-web`, added 2026-10-10) now blocks it too, but lives outside
   the repo.
5. **nginx config re-applied on every deploy.** The precedent skips the
   step once certbot has added `ssl_certificate`, so the repo file goes
   stale after the first deploy. Instead, every deploy:
   1. backs up the live site file (if any);
   2. copies the repo file into `sites-available` and symlinks it;
   3. no certificate yet → `certbot --nginx -d api.crockpot.app
      --non-interactive --agree-tos --email … --redirect` (issues);
      certificate exists → `certbot install --cert-name api.crockpot.app
      --nginx --redirect` (reinstalls, no issuance, no Let's Encrypt rate
      limit);
   4. `nginx -t`, then reload.
   **Any failure from step 2 on** restores the backup (first deploy: removes
   the file and symlink), then `nginx -t && systemctl reload nginx`, then
   exits 1. The nginx is shared with three other apps; a broken file left
   on disk would fail their next reload.
6. **One env file.** The deploy step writes `~/crockpot-api.env` from the
   secrets with a quoted heredoc (`<<'EOF'`), `chmod 600`; both the deploy
   and rollback `docker run` use `--env-file`. One list instead of two
   that can drift (a variable missing from the rollback list fails
   `config.validate` exactly when rollback is needed), and the env-file
   format takes values literally, so a `'` in a secret no longer breaks
   the command. No new exposure: Docker already stores `-e` values in the
   container config.
7. **Rollback reports honestly, to the last image that was serving.**
   (Revised at branch review: the precedent's registry `:previous` is
   "whatever `:latest` was at build time", so after one failed deploy a
   second failure would roll back to the broken image.) Before stopping
   the old container, the deploy step tags its image as the local
   `crockpot-api:rollback` on the droplet, but only if `/health`
   answers, so an unhealthy container is never promoted. Deploys pull
   `:${{ github.sha }}`, not `:latest`; the build job no longer tags
   `:previous`. After a rollback, the same `/health` poll runs against
   it: healthy → "rolled back to the previous image"; unhealthy →
   `::error::Rollback also failed — the schema is probably newer than
   the previous image. Fix forward.` Cause: migrations run at startup
   (`db/db.go:93`). A failed container is stopped and kept as
   `crockpot-api-failed`; container logs never go to the job output (the
   repo is public). Old SHA images are removed after a healthy deploy.
8. **Deploys queue**: `concurrency: { group: production_environment,
   cancel-in-progress: false }`. Cancelling kills the SSH session midway,
   after the old container is stopped or between the nginx copy and its
   restore.
9. **`client_max_body_size 7m`.** The app's recipe-write cap is 6 MiB
   (`main.go:37`) and returns JSON `413 request_too_large`
   (`internal/handler/recipe_requests.go:58-60`); nginx at `6m` would
   answer first with an HTML 413. At `7m` the app is the only limit
   anyone hits.
10. **Port 5600** (5300/5400/5500 taken), confirmed free in the
    prerequisites.
11. **Unchanged from the precedent**: multi-stage `golang:1.26-alpine` →
    `gcr.io/distroless/static`; build/deploy gated on `checks` and a push
    to `main`; host-side `/health` poll; the
    `production` GitHub Environment. CI's `checks` job keeps its Postgres
    service container. The swap stops the old container before starting
    the new one (shared port), so each deploy has ~15–45s downtime;
    accepted.

12. **Found while building** (not in the grill):
    - `appleboy/ssh-action` no longer has a `script_stop` input
      (v1.2.5's `action.yml`), so the precedent's `script_stop: true` on
      `@master` is ignored. Actions pinned (`ssh-action@v1.2.5`,
      `scp-action@v1.0.0`) and every script starts with `set -e`.
    - The precedent's Health Check doesn't run if the Deploy step fails
      partway (old container already stopped). It now runs whenever
      Deploy ran, and handles each state: new container present →
      health-poll then swap or roll back; only the old one → "still
      serving", no change; neither → roll back.
    - The Deploy step pulls the new image before stopping the old
      container, shortening the downtime window.
13. **Branch-review hardening**: every action outside `actions/*`
    pinned to a commit SHA (current tags kept: `docker/*` v3/v5,
    `appleboy/*` v1.2.5/v1.0.0); host key verified via
    `fingerprint: ${{ secrets.DO_HOST_FINGERPRINT }}` (the action
    negotiates the droplet's ECDSA key, checked with x/crypto v0.45.0,
    the version `ssh-action@v1.2.5` ships); `curl --max-time 5` in every
    health poll; final image `gcr.io/distroless/static:nonroot` (uid
    65532).

Droplet paths use `$HOME`, not the precedent's `/home/${DO_USERNAME}`:
`DO_USERNAME` is `root`, whose home is `/root`.

Values: image `docker.io/<DOCKER_USERNAME>/crockpot-api`, container
`crockpot-api`, domain `api.crockpot.app`, `APP_ENV=production`,
`PORT=5600`, `CLOUDINARY_UPLOAD_FOLDER=recipes`,
`TRUSTED_PROXIES=172.17.0.1` (droplet bridge gateway, read 2026-10-10).

## Prerequisites (founder, before merging)

1. **Cloudflare DNS**: check what `api.crockpot.app` points at now; set an
   A record `api` → droplet IP, proxied. SSL/TLS mode Full (Strict). Leave
   the apex alone (Vercel, `CROC-077`). SSL/TLS → Edge Certificates →
   "Always Use HTTPS" off: on, it redirects certbot's HTTP-01 challenge
   to HTTPS, which 526s before a cert exists and fails every renewal.
   nginx redirects to HTTPS itself.
2. **Retire the old Node API** (`../crockpot-api`): it was deployed to
   `api.crockpot.app` on 5100 under the same container (`crockpot-api`)
   and Docker Hub image names. On the droplet: remove its container and
   images, its nginx site file, and `certbot delete --cert-name
   api.crockpot.app` (a stale cert would be reinstalled by decision 5
   and 526 under Full (Strict)). Delete the `franciskershaw/crockpot-api`
   Docker Hub repo (else the first build tags the Node image
   `:previous`). Archive the GitHub repo (its `deploy.yml` redeploys on
   any push to `main`).
3. **Droplet**: `sudo ss -tlnp | grep 5600` is empty;
   `docker network inspect bridge` → note the gateway IP for
   `TRUSTED_PROXIES`.
4. **Droplet exposure check** — done 2026-10-10. No cloud firewall
   existed: 5400/5300/5500 answered on the public IP. Added DO Cloud
   Firewall `droplet-web` (inbound 22/80/443 only, outbound defaults);
   raw ports now time out, all three domains still 200. Decision 4's
   loopback publish stays as defence in depth.
5. **Neon** — done 2026-10-10. The `production` branch (default; parent
   of `development`) is empty (no tables in `public`) and is the prod
   database. IP Allow isn't on the plan; "Any IP address" is set. Restore
   window: 6 hours; manual snapshots available. Copy its **pooled**
   connection string (host with `-pooler`; `db/db.go` strips it for
   migrations) straight into `PROD_DATABASE_URL`.
6. **Google Cloud Console** — done 2026-10-10. Project
   `crockpot-api-final` is the one client for dev and prod; it already
   lists `http://localhost:8080/auth/google/callback` and
   `https://api.crockpot.app/auth/google/callback`. Consent screen stays
   in Testing until `CROC-077`; add the founder's Google account under
   Audience → Test users so Google sign-in can be checked after deploy.
   The old app's `crockpot` project retires at `CROC-078`.
7. **GitHub** (`crockpot-go` → Settings → Environments → `production`,
   deployment branches restricted to `main`). Environment secrets:
   `DO_HOST`, `DO_USERNAME`, `DO_SSH_KEY`, `DOCKER_USERNAME`,
   `DOCKER_PASSWORD`, `CERTBOT_EMAIL`, `PROD_DATABASE_URL`,
   `JWT_SECRET_ACCESS`, `JWT_SECRET_REFRESH`, `JWT_SECRET_OAUTH_STATE`
   (each fresh, `openssl rand -base64 32`, never `packing-list-go`'s),
   `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URI`
   (`https://api.crockpot.app/auth/google/callback`), `FRONTEND_URL`
   (`https://crockpot.app` or the `www` form; final value is `CROC-077`'s
   apex/`www` choice), `RESEND_API_KEY`, `EMAIL_FROM`,
   `CLOUDINARY_CLOUD_NAME`, `CLOUDINARY_API_KEY`, `CLOUDINARY_API_SECRET`.
   `TRUSTED_PROXIES` (step 3's gateway IP) and `CLOUDINARY_UPLOAD_FOLDER`
   are workflow `env` values, not secrets, as the precedent does.
   `DO_HOST_FINGERPRINT`: on the droplet, `ssh-keygen -l -E sha256 -f
   /etc/ssh/ssh_host_ecdsa_key.pub`; the secret is the `SHA256:…` field
   only.

## Acceptance criteria

- [ ] `Dockerfile` and `.dockerignore` as the precedent; `.dockerignore`
      also excludes `requests`, `scripts`, `.githooks`, `.github`.
- [ ] `nginx/crockpot-api.conf`: `server_name api.crockpot.app`, proxy to
      `127.0.0.1:5600`, Cloudflare `set_real_ip_from` (v4 + v6) and
      `real_ip_header CF-Connecting-IP`, `X-Real-IP`/`X-Forwarded-For` set
      to `$remote_addr`, `client_max_body_size 7m`.
- [ ] `ci.yml` gains `build` and `deploy` jobs per decisions 4–8 and 11:
      loopback publish, nginx re-apply with restore-on-failure, env file
      used by both runs, rollback health-polled with the honest error,
      `cancel-in-progress: false`.
- [ ] `actionlint` clean on `ci.yml`; every SSH script block passes
      `bash -n`.
- [ ] Prerequisites done.
- [ ] First deploy passes its health check; `https://api.crockpot.app/health`
      returns 200 in a browser with a valid certificate.
- [ ] `docker logs crockpot-api` shows the founder's own public IP for a
      request from their browser: not `172.17.x.x`, not a Cloudflare
      address.
- [ ] `curl -m5 http://<droplet-ip>:5600/health` from a laptop times out
      or is refused.
- [ ] A second deploy (any trivial merge) passes, its log shows
      `certbot install` (no new certificate), and the live site file
      matches the repo file plus certbot's SSL lines.
- [ ] `ls -l ~/crockpot-api.env` on the droplet shows `-rw-------`.

## Runbook: failed deploy

- **"rolled back to the previous image"**: the site is up on the old
  image. On the droplet, `docker logs crockpot-api-failed`; fix, merge
  again.
- **"Rollback also failed"**: `docker logs crockpot-api` on the droplet.
  A migration from the failed image is almost
  certainly applied. Either push a fix (preferred), or run that
  migration's `down` against prod by hand and re-run the last good
  deploy.
- **nginx step failed**: the previous site file was restored and nginx
  reloaded; the container was not touched.

## Non-goals

- Separating migrations from app startup, or a schema-aware rollback.
- Zero-downtime deploys.
- Cloudflare Origin CA certificates (cleaner, but diverges from the other
  three apps and adds a secret).
- Fixing `packing-list-go`'s equivalent issues (`TRUSTED_PROXIES`,
  `0.0.0.0` publish, stale nginx file, duplicated env list): flagged
  there, not changed here.
- Anything in `CROC-077`: the frontend, the apex/`www` records, Vercel,
  Resend domain verification, the consent screen, data.
- Scrubbing the OAuth `code` from request logs (security finding 4,
  accepted).

## Verification modes

- **Limits, thresholds, config — through the real client**: the
  first-deploy, real-IP log, public-port, second-deploy and env-file
  checks in the acceptance criteria. The real-IP check is the one that
  proves decision 2; nothing synthetic does.
- **Workflow structure, before merge**: `actionlint .github/workflows/ci.yml`;
  each `script:` block extracted and run through `bash -n`.
- **No Go tests**: no Go code changes.
- **Branch review** (`branch-review` against `main`) stands in for the
  deploy-config security pass: secrets handling (heredoc, file mode, what
  reaches job logs), the real-IP chain, the body limit, the loopback
  publish.
- **Rollback, locally** (not on the droplet): the Deploy and Health
  Check scripts chained against OrbStack across five successive
  deploys, registry pulls shimmed: first deploy, good over good (old
  images cleaned), broken over good (rolled back, failed container
  kept), broken again while rolled back (still rolls back to the good
  image), broken with a broken rollback image (honest error). Done
  2026-10-10, after the branch-review fixes.
- **Not exercised**: the nginx step's restore path (needs certbot and
  the shared nginx). The second deploy proves the `certbot install`
  path; a failure there restores and fails safely.

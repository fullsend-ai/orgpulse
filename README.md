# OrgPulse

OrgPulse (shown as "Pulse" in the UI) is built as `orgpulse` and deployed as `devlake-dashboard`: the container image and deployment resources keep that name. The Go module is `github.com/fullsend-ai/orgpulse` and the local build produces `bin/orgpulse`.

The dashboard for Red Hat's Leading Metrics Initiative — PR Cycle Time, Issue Cycle Time, AI Commit Scanner, Agent Ready, and Code Coverage, plus an AI chatbot for ad-hoc queries. It's backed by Apache DevLake and serves both the dashboard frontend and its management/REST API from a single Go binary.

## Configuration

All configuration is via environment variables.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | no | `9090` | Management server listen port |
| `ENABLE_PPROF` | no | `false` | Expose `/debug/pprof` endpoints |
| `DASHBOARD_DB_HOST` | no | `localhost` | MySQL host for configuration database |
| `DASHBOARD_DB_PORT` | no | `3306` | MySQL port |
| `DASHBOARD_DB_USER` | no† | — | MySQL user |
| `DASHBOARD_DB_PASSWORD` | no† | — | MySQL password |
| `DASHBOARD_DB_NAME` | no† | `dashboard` | MySQL database name |
| `DASHBOARD_DB_TLS_CA_BUNDLE_PATH` | no | — | Path to a PEM CA bundle **file**. When set, the MySQL connection uses TLS and verifies the server certificate against this CA (e.g. required by RDS instances with `require_secure_transport` enabled). When unset, the connection is unencrypted, same as before this existed — safe for local dev against a MySQL that doesn't enforce TLS. |
| `DASHBOARD_ADMIN_API_KEY` | no | — | Optional bootstrap admin API key for headless/scripted programmatic access |
| `CONFIG_ENCRYPTION_KEY` | no | — | 32-byte key, base64 or hex (`openssl rand -base64 32`). Encrypts stored integration and personal tokens (AES-256-GCM); existing plaintext tokens are encrypted at startup. Once set, the server refuses to start without the same key. To remove or change it, first run `migrate decrypt-secrets` with the current key. Unset: tokens are stored in plaintext (logged as a warning) |
| `ADMIN_USERS` | no | — | Comma-separated list of usernames (or `*`) granted Admin privileges via OAuth proxy |
| `AUTH_EMAIL_DOMAINS` | no | — | Comma-separated email domains (e.g. `example.com`). When set, a bare name in `ADMIN_USERS` or a Rover export only matches the local part of emails in these domains; exact usernames and full emails still match. When unset, the local part of any domain matches (logged as a warning at startup) |
| `DEV_ADMIN_OVERRIDE` | no | `false` | Treat all requests, including unauthenticated ones, as Admin. Local development only; logged as a warning at startup |
| `AUTH_TEST_MODE` | no | `false` | Bypass forwarded-identity auth for local development (no proxy sidecar needed). Logged as a warning at startup |
| `AUTH_TEST_EMAIL` | no | `testuser@example.com` | Synthetic identity used when `AUTH_TEST_MODE=true` |
| `AUTH_TEST_IS_ADMIN` | no | `false` | Grant Admin privileges to synthetic identity when `AUTH_TEST_MODE=true` |
| `AUTH_TEST_GROUPS` | no | — | Comma-separated Rover groups for the `AUTH_TEST_MODE` identity. Used when no membership file is mounted |
| `ROVER_GROUPS_CSV` | no | — | Path to one Rover membership CSV, or a directory of them. A Rover export has no group column; the group name is the filename without `.csv`. Reloaded when the files change. Do not commit a real export |
| `OPERATOR_ROVER_GROUPS` | no | — | Comma-separated Rover groups that can see and update every configured org, including Rover groups. Operators are not platform admins |
| `PUBLIC_API_URL` | no | — | External base URL for programmatic API access, shown next to API keys in Admin → API Keys. When unset, a host named `devlake-dashboard…` is shown as `devlake-dashboard-api…` |
| `AI_ANALYTICS_URL` | no§ | — | AI endpoint for per-element analysis (receives JSON, returns HTML) |
| `AI_SUMMARY_URL` | no§ | — | AI endpoint for executive summary (receives JSON, returns HTML) |
| `SMTP_HOST` | no§ | — | SMTP server address for report emails |
| `SMTP_PORT` | no | `25` | SMTP server port |
| `SMTP_FROM` | no§ | — | Sender email address for report emails |
| `SUBSCRIPTION_CHECK_INTERVAL_SECONDS` | no | `3600` | How often the scheduler checks for due subscription reports |
| `ALERT_CHECK_INTERVAL_SECONDS` | no | `3600` | How often the scheduler checks alerts |
| `SUBSCRIPTION_SPREAD_SECONDS` | no | `14400` | Time window over which due subscriptions are spread to avoid bursts |
| `ALERT_SPREAD_SECONDS` | no | `7200` | Time window over which alert checks are spread to avoid bursts |

 †All three of `DASHBOARD_DB_USER`, `DASHBOARD_DB_PASSWORD`, and `DASHBOARD_DB_NAME` must be set to enable the configuration database. The schema is **not** auto-created — run `make migrate` (or the pre-deploy Job in production) first; see [docs/migrations.md](docs/migrations.md). API keys are optional and reserved for headless/scripted programmatic access (e.g. CI pipelines, curl, external scripts); browser users in OpenShift/production are authenticated via OAuth proxy and `ADMIN_USERS`, and in local dev via `AUTH_TEST_MODE` / `DEV_ADMIN_OVERRIDE`. Set `DASHBOARD_ADMIN_API_KEY` only if you need to bootstrap a static key for programmatic access on first run.

§All four of `AI_ANALYTICS_URL`, `AI_SUMMARY_URL`, `SMTP_HOST`, and `SMTP_FROM` plus a configured database are required to enable the report service (`POST /sendreport`). Without them, the endpoint returns 503.

‖The dashboard trusts `X-Forwarded-User`/`X-Forwarded-Email` headers set by a fronting `ose-oauth-proxy-rhel9` sidecar as proof of an authenticated browser session for `/api/v1/*` — this is only safe when the app is reachable *exclusively* through the proxy's port, never directly via the Service/Route. Without a proxy in front (and without `AUTH_TEST_MODE`), CRUD features require a static API key instead. `X-Forwarded-Email` isn't guaranteed to be populated by every OpenShift OAuth identity provider; code that reads it tolerates it being empty.

## Endpoints

The management server (default `:9090`) exposes:

| Path | Description |
|------|-------------|
| `/` | Dashboard frontend — vanilla JS (embedded from `web/static/`) |
| `/` | Dashboard frontend — PatternFly 6 (embedded from `web/patternfly/build/`) |
| `/health/live` | Liveness probe — always returns 200 with `{"status":"ok"}` |
| `/health/ready` | Readiness probe — `{"status":"ok"}`, or 503 `{"status":"not_ready"}` when a dependency is unreachable |
| `/metrics` | Prometheus Go runtime metrics |
| `/api/config` | Frontend configuration (AI analytics URL, forwarded caller identity) |
| `/api/orgs` | Organization config as JSON (only when DB is configured) |
| `/api/products` | Product config as JSON (only when DB is configured) |
| `/api/teams` | Team config as JSON (only when DB is configured) |
| `/api/v1/*` | REST API for CRUD operations (requires an API key, or a proxy-forwarded identity; only when DB is configured) |
| `/debug/pprof/*` | Go pprof (only when `ENABLE_PPROF=true`) |

See [`docs/rest-api.md`](docs/rest-api.md) for the REST API, [`docs/orgpulse-user-guide.md`](docs/orgpulse-user-guide.md) for onboarding and metrics, and [`docs/dashboard/ai-analytics-system-prompt.md`](docs/dashboard/ai-analytics-system-prompt.md) for the AI analytics prompt.

## Development

Requires Go 1.26+ and Node.js 24+ (for the PatternFly frontend).

```sh
make build            # build PatternFly frontend + Go binary (bin/orgpulse)
make frontend-build   # build the PatternFly frontend only
make test             # run tests with race detector (no cache)
make test-e2e         # DB_TLS_CA_BUNDLE_PATH against a real MySQL container enforcing
                      # require_secure_transport (requires podman; needs a nested
                      # container, not run in CI -- see docs/e2e-testing.md)
make vet              # run go vet
make fmt-check        # verify gofmt formatting
make lint             # run golangci-lint (requires golangci-lint)
make run              # build and run the dashboard locally
make query            # query /health/ready on the running dashboard
make dev              # full local dev environment with mock data (MySQL + mock-diagrams + dashboard)
make dev-down         # tear down the environment started by `make dev`
make create-admin-key # generate an admin API key in the database
make import-config    # import config JSONs into the database
make clear-config     # delete all config data (preserves API keys)
make clean            # remove build artifacts (Go + frontend)
make help             # show all targets
```

### Frontend development

The frontend lives in `web/patternfly/` and is built with PatternFly 6, React, TypeScript,
and Vite.

For local development with hot-reload:

```sh
cd web/patternfly
npm install
npm run dev
```

This starts the Vite dev server with a proxy that forwards `/api` requests to the Go
backend on `:9090`. Run `make run` in a separate terminal to start the backend.

## Running locally

```sh
make run
```

Or, to enable the database/admin features:

```sh
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
make run
```

This builds the binary and starts the dashboard using the environment variables. Then in another terminal:

```sh
make query
```

This prints the `/health/ready` response. Use `make query PORT=8080` to target a different port.

### Seeing real data on the dashboard

With no database configured, `/` renders but shows no org/product/team cards, and the
Settings tabs show a "requires an authenticated session" message — that's expected, not a bug. The landing page reads
its navigation from `/api/orgs`, `/api/products`, `/api/teams`, which only exist when a
database is configured; and Settings' gated tabs (general, alerts, subscriptions, my teams)
require a logged-in session.

The easiest way to see all of this working with sample data:

```sh
make dev
```

This starts a full local environment via `compose-dev.yaml`: MySQL, a `mock-diagrams`
service (`cmd/mock-diagrams/`, returns a canned chart payload for every request), and the
dashboard itself pre-seeded with a test org/product/team, a subscription, and an alert,
with `AUTH_TEST_MODE=true` so the gated Settings tabs render. Visit
`http://localhost:9090`. Stop everything with `make dev-down`.

Building this environment without compose, piece by piece:

```sh
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
export AUTH_TEST_MODE=true
make seed-test-data   # populates a test org/product/team, a subscription, and an alert
make run
```

`AUTH_TEST_MODE=true` bypasses forwarded-identity auth so the gated Settings tabs render
without a real proxy sidecar in front (see `AUTH_TEST_EMAIL` to change the test identity).

Note this only reproduces the dashboard's own configuration (orgs/products/teams,
subscriptions, alerts) — the actual chart data (PR Cycle Time, Issue Cycle Time, etc.)
is sourced from Apache DevLake's database via a separate data pipeline that lives outside
this repo (or `mock-diagrams`, when using `make dev`), so charts won't populate with real
numbers from a bare local checkout without one or the other.

## Creating an admin API key

API keys are **optional** and reserved for headless or scripted programmatic access (e.g. CI/CD pipelines, curl, external scripts, workflow automation).
- In **OpenShift/production**, browser users are automatically authenticated via the `ose-oauth-proxy` sidecar and granted Admin privileges via `ADMIN_USERS`. No API key generation is needed for browser UI access (including Settings and `/admin/`).
- In **local development** (`make dev`), `AUTH_TEST_MODE=true` and `DEV_ADMIN_OVERRIDE=true` automatically grant access without keys.

If you need a static API key for programmatic automation, generate an admin key directly in the database:

```sh
export DASHBOARD_DB_HOST=localhost DASHBOARD_DB_PORT=3306
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
make create-admin-key
```

This connects to the database (which must already be migrated — see [docs/migrations.md](docs/migrations.md)), generates a random admin API key, and prints it to stdout. Save the key — it cannot be retrieved later.

Use the key to authenticate against the REST API (`/api/v1/*`) or as a fallback for the admin UI (`/admin/`). See [`docs/rest-api.md`](docs/rest-api.md) for details.

## Importing configuration

Import configuration from a directory of JSON files (matching the JSON schemas in `schemas/`):

```sh
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
make import-config DIR=path/to/data
```

The directory should contain subdirectories `orgs/`, `products/`, and `teams/`, each with `.json` files — one entity per file in the assembled JSON format. Missing subdirectories are skipped. Import order respects foreign key constraints (teams first, then products, then orgs).

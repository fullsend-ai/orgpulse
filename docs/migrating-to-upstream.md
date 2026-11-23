# Migrating to the upstream release

This guide describes compatibility considerations when upgrading from an
earlier devlake-dashboard build to OrgPulse.

## Unchanged (no action)

- HTTP routes and payloads, except the generic error text listed under
  behaviour changes.
- Environment variable names and defaults, including `PORT`,
  `ENABLE_PPROF`, `DASHBOARD_DB_*`, `DASHBOARD_ADMIN_API_KEY`, `ADMIN_USERS`,
  `AUTH_*`, `ROVER_GROUPS_CSV`, `OPERATOR_ROVER_GROUPS`, `PUBLIC_API_URL`,
  `AI_*`, `SMTP_*`, `SUBSCRIPTION_*`, `ALERT_*`, `DEVLAKE_*`, `TLS_SKIP_VERIFY`.
- Metric names and labels (`go_*` and `process_*` from the Prometheus
  collectors).
- Container entrypoint `/orgpulse` and listen port 9090.
- Database schema and migrations.

## Changed

| Change | Old | New | Action |
|---|---|---|---|
| Go module path | `orgpulse` | `github.com/fullsend-ai/orgpulse` | Only for code that imports packages; `internal/` is not importable externally |
| Local build output | `bin/devlake-dashboard` | `bin/orgpulse` | Update local scripts and developer muscle memory |
| Licence | MIT | Apache-2.0 | Redistributors review `NOTICE` |
| Go version | 1.25 | 1.26.10 | Build-only; run the image as before |
| Frontend build runtime | Node 22 | Node 24 | Build-only |
| Base images | floating UBI9 tags | digest-pinned UBI9 (`go-toolset:1.26.10`, `nodejs-24`, `ubi-minimal:9.8`) | No action unless you mirror images |
| MCP server name | `devlake-dashboard` | `orgpulse` | Only MCP clients that key on `serverInfo.name` |
| VS Code extension metadata | Earlier repository URLs, MIT | GitHub URLs, Apache-2.0 | Republish only if you ship the VSIX |

## Behaviour changes

All status codes are unchanged.

- Error responses keep their status codes and JSON shapes. Which callers see
  internal detail depends on the route:

| Route or class | Callers | Response | Detail location |
|---|---|---|---|
| `POST /chart` | authenticated | generic `render error`; response-write failures are logged only | log with request id |
| `POST /sendreport` | admin | generic `report error` or `response encode error` | log only, with request id |
| Onboarding, stored-procedure CRUD, workflow publish | authenticated and permission-checked | generic message; administrators get `message: cause` | response for administrators; the log always carries the request id |
| `POST /api/v1/my-issues/transition` | authenticated | `502` with `transition failed` | log only |
| SQL console, DevLake DB query/tables, run-stage, workflow test results | admin-only | unchanged `200` with engine `error` fields | response and log |
| DevLake sync/trigger, Codecov availability, workflow import, import-team | admin-only | unchanged status codes with `operation: cause` on failure | response and log |
| DevLake/Jira proxy endpoints, including `/proxy-browse`, `/proxy-jira-meta`, `/proxy-jira-project`, `/proxy-jira-board-meta` and the team Jira board metadata route | authenticated, not admin | `502` with `<operation>: <upstream cause>` | response and log |
| `POST /api/v1/devlake/test-connection` | authenticated, not admin | unchanged `200` with `{"success":false,"message":"<cause>"}` | response and log |

- Invalid chart specifications still return 400, report failures still return
  500, missing subscriptions still return 404, and Jira transition failures
  still return 502.
- The connection-test message and the proxy-endpoint causes describe the
  caller's own endpoint and credentials and are retained for compatibility with
  `DevLakeDBPanel.tsx` and the VS Code extension. They are accepted disclosure
  exceptions, not admin-only routes.
- Onboarding, stored-procedure write, workflow publish, sync/trigger, workflow
  import and import-team failures return the full cause to administrators and
  never to non-admin callers; the request id is in the log. Non-admin responses
  append the request id to the message.
- Admin-only SQL console endpoints and workflow test results continue to
  return raw engine errors; that is intentional admin feedback.
- An invalid `PORT`, `ENABLE_PPROF` or `DASHBOARD_DB_PORT` still exits with
  status 1 and the same `invalid value for <KEY>: <value>` message, but the
  error is now printed before logging is configured.
- The startup log line is now `starting orgpulse` instead of
  `starting devlake-dashboard`. Update any log-based alert or saved search that
  matches the old text.
- The development-only fallback identity used when `DEV_ADMIN_OVERRIDE` is on
  without forwarded headers is now `dev-admin@example.com`. Subscriptions or
  alerts created by the previous fallback identity keep the old address;
  re-point them if needed.

## Verification after rollout

```sh
curl -fsS http://<host>:9090/health/live
curl -fsS http://<host>:9090/health/ready
curl -fsS http://<host>:9090/metrics | grep -c '^go_'
curl -fsS -o /dev/null -w '%{http_code}\n' http://<host>:9090/
```

Expected: `{"status":"ok"}` from the liveness endpoint, a non-zero `go_`
metric count, and `200` from the dashboard root. Readiness returns
`{"status":"ok"}` when the configured dependencies are healthy and
`{"status":"not_ready"}` with HTTP 503 otherwise. `/debug/pprof/` must not
serve pprof output unless `ENABLE_PPROF=true`.

## Rollback

Redeploy the previous image after confirming it is compatible with the
database schema and current configuration. See [migrations.md](migrations.md)
for database migration and encrypted-credential rollback considerations.

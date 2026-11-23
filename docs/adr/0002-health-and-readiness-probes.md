# ADR: Health and Readiness Probes

**Date:** 2026-09-07  
**Status:** Approved  

---

## Context & Motivation

OrgPulse needs separate liveness and readiness checks for service orchestration.

### Current State

- Single monolithic `/health` endpoint that combines liveness and readiness checks
- No standardized health probe endpoints

### Problems to Solve

1. **False readiness signals**: A single `/health` endpoint that checks all dependencies causes false negatives when external services are unavailable, leading to cascading restarts even when the dashboard itself is healthy.

---

## Endpoint Structure

| Endpoint | Purpose | Dependencies | Response |
|----------|---------|--------------|----------|
| `GET /health/live` | Liveness probe | None (process vitality only) | `200 {"status": "ok"}` |
| `GET /health/ready` | Readiness probe | Database, Workflows service (if configured) | `200 {"status": "ok"}` or `503 {"status": "not_ready"}` |
| `GET /health` | Removed | — | `404 Not Found` |

### Implementation Details

#### `/health/live` (Liveness)

- **Purpose**: Determine if the process is alive and responding to HTTP requests
- **Checks**:
  - Process is running
  - HTTP listener is accepting connections
- **Timeout**: No timeout (should respond immediately)
- **Response**: `HTTP 200 {"status": "ok"}`

#### `/health/ready` (Readiness)

- **Purpose**: Determine if the service is ready to serve traffic
- **Timeout**: 2 seconds (to prevent probe delays from masking issues)
- **Checks**:
  1. **Dashboard MySQL store**: Use `sql.DB.PingContext(ctx)` with 2-second timeout
  2. **DevLake DB**: Use `sql.DB.PingContext(ctx)` with 2-second timeout (if configured)
  3. **Workflows service**: `GET <WORKFLOW_EXEC_URL>/health/live` with 2-second timeout (only if `WORKFLOW_EXEC_URL` is set)
- **Response**:
  - All checks pass: `HTTP 200 {"status": "ok"}`
  - Any check fails or times out: `HTTP 503 {"status": "not_ready"}`

### Deployment Updates

#### Kubernetes example

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 9090
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 2
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /health/ready
    port: 9090
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

#### Docker Compose (compose-dev.yaml)

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:9090/health/ready"]
  interval: 10s
  timeout: 3s
  retries: 3
  start_period: 10s
```

#### Makefile

Update the `health` target to use `/health/live` for liveness checks.

---

## Testing Strategy

### Unit Tests

1. **Health Probes** (`internal/server/health_test.go`):
   - Test `/health/live` returns 200 with simple payload
   - Test `/health/ready` succeeds when all dependencies are healthy
   - Test `/health/ready` fails when database is unreachable
   - Test `/health/ready` fails with timeout
   - Test `/health` returns 404

### Manual Verification

```bash
# Test liveness
curl -v http://localhost:9090/health/live

# Test readiness (should succeed with healthy DB)
curl -v http://localhost:9090/health/ready
```

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKFLOW_EXEC_URL` | (optional) | Workflows service URL for readiness checks |

---

## Rollout Plan

1. Implement and deploy `/health/live` and `/health/ready` endpoints
2. Update Kubernetes deployments to use new endpoints

---

## Rollback Plan

If issues are detected:

1. Revert deployment to previous version
2. Monitor `/health/live` and `/health/ready` endpoints
3. Check application logs for errors

---

## Future Enhancements

1. Add `/health/startup` endpoint for slow-starting services

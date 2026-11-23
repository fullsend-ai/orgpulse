# ADR: Structured Logging and Request Tracing

**Status:** Accepted

## Context

Operators need to correlate HTTP requests with application logs while keeping
routine health checks from overwhelming the logs.

## Decision

OrgPulse uses Go's `log/slog` for structured logging. Configure `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) and `LOG_FORMAT` (`text` or `json`);
the defaults are `info` and `text`.

The request middleware accepts an `X-Request-ID` containing 1–128 letters,
digits, dots, underscores, colons or hyphens. If it is absent or invalid,
the middleware generates an ID. The response includes the ID, and the
request context makes it available to logging and downstream calls.

Completed request logs include the method, path, status, duration, bytes
written and request ID. Successful requests to `/health/live`, `/health/ready`
and `/metrics` are not logged; unsuccessful requests remain visible.

## Verification

The logging and request middleware are covered by tests in `internal/logging/`
and `internal/server/`. The integration tests include request propagation.

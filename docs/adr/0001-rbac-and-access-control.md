# ADR: Authentication and Access Control

**Status:** Accepted

## Context

OrgPulse serves browser users and programmatic clients. It needs to identify
callers, restrict administrative operations, and limit tenant data to the
caller’s authorization grants.

## Decision

Browser access uses an authenticating proxy that forwards `X-Forwarded-User`
and `X-Forwarded-Email`. The application trusts those headers. Deployments
must prevent clients from reaching the application directly and configure the
proxy to replace client-supplied identity headers.

Programmatic clients can authenticate with an API key in the `X-API-Key` or
`Authorization: Bearer` header. An admin API key grants administrative
access. A non-admin key needs a scoped user to access tenant data.

`ADMIN_USERS` identifies administrators. `AUTH_EMAIL_DOMAINS` restricts
email-local-part matching to configured domains. Group membership and
`auth_group` assignments determine which organizations, products and teams a
non-admin can access; `OPERATOR_ROVER_GROUPS` identifies operators.

`AUTH_TEST_MODE` and `DEV_ADMIN_OVERRIDE` are intended for local development
and testing only. The latter treats every request as an administrator.

## Consequences

The deployment must provide and secure the authenticating proxy and configure
identity and group membership. Administrative and tenant access are enforced
by the application, independently of which frontend view is visible.

See the [REST API reference](../rest-api.md#authentication) for roles,
credentials and authorization behavior, and the [tenant grant ADR](0002-tenant-rover-grants.md)
for group-based visibility. See [SECURITY.md](../../SECURITY.md) for deployment
hardening.

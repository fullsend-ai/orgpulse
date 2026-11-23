# ADR: Tenant access from Rover groups

## Status

Accepted (2026-10-27)

## Context

SSO (`X-Forwarded-User` / `X-Forwarded-Email`) proves who the caller is. It does not say which organization, product, or team they may see. Each org, product, and team already stores a Rover group in `auth_group`. The onboarding wizards collect that value. Until this change, the assembled config endpoints and the live workflow routes returned data for every team.

A live Rover API client is follow-up work. The first deployment can use a membership file exported from Rover.

## Decision

Membership for this release is the path in `ROVER_GROUPS_CSV`: one CSV, or a directory of Rover exports. Rover exports one group per file and does not include a group column; the group name is the filename with `.csv` removed. A file that already has a group column still uses that column. The process loads the path at startup and reloads it when a file's size or modification time changes, or when a file is added or removed. A file that cannot be parsed contributes no memberships. An unreadable path grants no tenant access. A real export is mounted beside the process and is not committed. Tests use a fake fixture in the Rover shape.

The CSV is behind a `Directory` interface (`Groups(user) []string`). Grant matching does not know whether the groups came from a file or, later, from Rover.

Matching is case-insensitive. The value stored in `auth_group` must be the Rover group name. A blank `auth_group` matches nobody. An unknown user, a missing CSV row, or a file that cannot be read gets no tenant access.

- A team group sees that team. Parent org and product names stay visible so the person can navigate. Sibling teams and product or org aggregate dashboards stay hidden.
- A product group sees that product and every team in it, including the product aggregate.
- An org group sees that org and everything under it.
- Grants union. Where they overlap, the wider level wins.
- `OPERATOR_ROVER_GROUPS` sees every configured org and can change Rover groups there. Operators are not platform admins.
- `ADMIN_USERS`, `DEV_ADMIN_OVERRIDE`, and `is_admin` API keys remain a full bypass.
- A non-admin API key is tenant-scoped only when `scoped_user` is set. Otherwise it sees no tenant data.
- `AUTH_TEST_GROUPS` supplies groups for `AUTH_TEST_MODE` so local development does not need a file.

Contact email remains the owner of subscriptions and alerts. It does not grant visibility. Deletes of orgs, products, and teams stay admin-only.

Live workflow routes authenticate and reject a metric body unless every repository and work-item project belongs to a granted team.

## Consequences

Phase 2 roles (contributor, manager, org leader, custom views) and a Rover API client are out of this change. Replacing the CSV means implementing `Directory`; the grant rules stay the same.

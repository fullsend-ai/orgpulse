# Database Migrations

The configuration database schema is versioned with [goose](https://github.com/pressly/goose)
SQL migrations, applied by the standalone `cmd/migrate` binary. `NewMySQLStore`
(used by the dashboard and the other `cmd/*` admin tools) never applies DDL
itself — it assumes the schema is already at the version the running binary
expects.

## Adding a migration

1. Add a new file to `internal/database/migrations/`, named
   `NNNNN_short_description.sql` with the next sequential number (e.g.
   `00013_add_widget_column.sql`).
2. Write it with goose's `-- +goose NO TRANSACTION`, `-- +goose Up`, and
   `-- +goose Down` markers:

   ```sql
   -- +goose NO TRANSACTION
   -- +goose Up
   ALTER TABLE teams ADD COLUMN widget_url TEXT NULL;

   -- +goose Down
   ALTER TABLE teams DROP COLUMN widget_url;
   ```

3. No `StatementBegin`/`StatementEnd` wrapper is needed unless a single
   logical statement contains its own semicolons (e.g. a stored procedure).
4. Migration files are embedded at build time (`internal/database/migrations/embed.go`)
   — no separate asset step required.

## MySQL 8 / InnoDB migration rules

MySQL and InnoDB have specific transactional and DDL behaviors that dictate
how schema migrations must be written and executed:

- **Implicit commit on DDL:** In MySQL, DDL statements (such as `CREATE TABLE`,
  `ALTER TABLE`, `DROP TABLE`) cause an implicit commit. Wrapping multiple DDL
  statements inside a transaction does not make them atomic as a group.
- **Statement-level atomicity:** MySQL 8 / InnoDB provides atomic DDL for
  individual supported statements (e.g. a single `ALTER TABLE` modifying
  multiple columns succeeds or fails as a single atomic operation), but it does
  not roll back earlier completed DDL statements if a subsequent statement in
  the same file fails.
- **Explicit `NO TRANSACTION`:** All SQL migrations containing DDL statements
  must explicitly specify `-- +goose NO TRANSACTION` at the top of the file so
  Goose does not attempt ineffective transaction management.
- **One atomic DDL operation per migration file:** Prefer exactly one
  logical/atomic DDL statement per migration file whenever practical.
  - **Same table:** When multiple changes target the same table, combine them
    into a single `ALTER TABLE` statement (e.g. `ALTER TABLE teams ADD COLUMN a INT, ADD COLUMN b INT;`)
    so MySQL executes the changes atomically.
  - **Different tables:** When changes target different tables, split them into
    separate sequential migration files. This ensures each applied DDL statement
    corresponds cleanly to one Goose version step.
- **Do NOT use `IF EXISTS` / `IF NOT EXISTS` to mask failures:** Do not use
  `IF EXISTS` or `IF NOT EXISTS` clauses on DDL statements to hide partial
  failures or mask inconsistent schema states. Schemas must be deterministic.
- **Down migrations:** Down migrations must follow the same atomicity, simplicity,
  and `NO TRANSACTION` rules as Up migrations.
- **Immutability of deployed migrations:** Existing migrations that have been
  deployed to staging or production environments must never be rewritten or
  modified casually. Schema updates or fixes must always be introduced as new
  forward migration files.

## Failure and recovery protocol

A failed migration during deployment must immediately halt deployment. Never
automatically force or advance Goose versions.

When recovering from a failed or partially applied migration:

1. **Halt and inspect real schema:** Identify the failed migration and inspect
   the actual database schema (using `SHOW CREATE TABLE`, `SHOW TABLES`, or
   querying `information_schema`) to determine the exact state of the database.
2. **Reconcile schema first:** Manually align the database schema with the
   desired state corresponding to the last known good migration (or manually
   complete the DDL statements for the failed version).
3. **Reconcile Goose version:** Only after the real schema matches the intended
   state, reconcile the `goose_db_version` table so Goose accurately reflects
   the actual schema version.
4. **Fix forward:** Correct any underlying issues (e.g. data anomalies, syntax
   errors) in migration scripts and roll out the fix via regular deployment.

## Running migrations

**Locally:** `make migrate` (requires `DASHBOARD_DB_HOST`, `DASHBOARD_DB_PORT`, `DASHBOARD_DB_USER`,
`DASHBOARD_DB_PASSWORD`, `DASHBOARD_DB_NAME`, and optionally `DASHBOARD_DB_TLS_CA_BUNDLE_PATH` — same
variables the dashboard itself uses). `make dev` runs this automatically via
the `migrate` service in `compose-dev.yaml`, before `seed` or `dashboard`
start.

**Pre-Goose databases:** if the database was created before Goose was
adopted (the baseline tables exist but `goose_db_version` does not), `make
migrate` automatically detects this and stamps migration `00001` as already
applied. Only migrations after the baseline will run. No manual intervention
is required.

**In production:** as a **pre-deploy Job**, not an initContainer on the
Deployment. The dashboard's own database has a single schema regardless of
how many DevLake instances/teams are configured (the dashboard talks to each
DevLake/sidecar over HTTP via each team's `fetch_url`, not by connecting to
their databases), so there's exactly one migration target — but the
dashboard Deployment can still run multiple replicas, and goose has no
built-in advisory locking for MySQL (unlike its Postgres session locker). If
`migrate` ran from an initContainer on every pod, a multi-replica rolling
update could run it concurrently from more than one pod and race on
`goose_db_version`. Running it once, before the Deployment is promoted,
avoids that entirely.

The Job's contract:

- **Image:** the same `<registry>/orgpulse:<tag>` image as the
  Deployment being rolled out — don't pin a different tag.
- **Command:** `["/migrate"]` (overrides the image's default
  `/orgpulse` entrypoint).
- **Env:** the same `DASHBOARD_DB_HOST`, `DASHBOARD_DB_PORT`, `DASHBOARD_DB_USER`, `DASHBOARD_DB_PASSWORD`, `DASHBOARD_DB_NAME`,
  and `DASHBOARD_DB_TLS_CA_BUNDLE_PATH` as the Deployment.
- **Gating:** the Deployment rollout must not proceed until the Job exits 0.
  Configure a pre-deploy hook or equivalent step in your deployment system.

## Credential encryption and rollback

With `CONFIG_ENCRYPTION_KEY` set, the dashboard stores `devlake_*_token` and
`personal_*_token` values in `entity_config` encrypted (`enc:v1:` prefix,
AES-256-GCM) and encrypts any remaining plaintext tokens at startup. Versions
without encryption support cannot read these values. Before rolling back to
such a version, or before removing or rotating the key, run the migrate binary
with the current key:

```sh
CONFIG_ENCRYPTION_KEY=<current key> /migrate decrypt-secrets   # in the image
CONFIG_ENCRYPTION_KEY=<current key> go run ./cmd/migrate decrypt-secrets
```

It writes every encrypted token back as plaintext. Then remove the key (or
start the old version); to rotate, set the new key and restart, which
re-encrypts. The dashboard refuses to start if encrypted values exist and the
key is missing or does not match.

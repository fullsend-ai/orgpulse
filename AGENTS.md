# Agents

Base rules for AI assistants working in this repository. Claude-Code-specific tool/skill
hygiene lives in [CLAUDE.md](./CLAUDE.md); this file is the source of truth for project
conventions and structure.

## Golden rules

1. **One message = all related operations** — batch reads, writes, shell commands, and
   agent spawns together.
2. **No working files in repo root** — root holds only `README.md`, `AGENTS.md`,
    `CLAUDE.md`, `LICENSE`, `NOTICE`, `CONTRIBUTING.md`, `SECURITY.md`,
   `.gitignore`, `.gitleaks.toml`, `env.example`, `go.mod`/`go.sum`, `Makefile`,
   `Containerfile`, `compose-dev.yaml`,
   `.golangci.yml`, and `devlake-api.json`. Code goes
   in `cmd/`, `internal/`, `web/`; docs go in `docs/`; workflow bundles go in `workflows/`.
3. **Prefer editing over creating** — edit existing files rather than creating new ones.
4. **Prefer Grep/Glob over shell find/grep** when searching.

## Project overview

OrgPulse (deployed as `devlake-dashboard`; the UI shows "Pulse") is the dashboard for Red Hat's Leading Metrics Initiative — PR Cycle
Time, Issue Cycle Time, AI Commit Scanner, Agent Ready, and Code Coverage, plus an AI
chatbot for ad-hoc queries. It's backed by Apache DevLake and serves both the dashboard
frontend and its management/REST API on `:9090` from a single Go binary.

## Repository structure

| Directory | Purpose |
|---|---|
| `cmd/orgpulse/` | Entry point (builds the `orgpulse` binary) — config from env vars, wires the server |
| `cmd/*` (other) | Standalone admin/ops binaries: `clear-config`, `clear-seeds`, `clear-workflows`, `create-admin-key`, `import-config`, `import-team` (see `docs/import-team.md`), `migrate` (applies DB migrations, see `docs/migrations.md`), `seed-test-data`, `mock-diagrams` (dev-only canned chart data, see `make dev`), `mcp-server` (see `docs/mcp-server.md`) |
| `web/static/` | Embedded vanilla JS dashboard frontend (`//go:embed`) |
| `web/patternfly/` | PatternFly 6 + React + TypeScript + Vite frontend (`//go:embed`). Built by `make frontend-build`; dev server via `cd web/patternfly && npm run dev` |
| `internal/server/` | Management HTTP server (`/health/live`, `/health/ready`, `/metrics`, `/debug/pprof`, REST API) |
| `internal/chart/` | Chart.js-compatible chart rendering to PNG |
| `internal/report/` | AI-powered report generation, email delivery, scheduler |
| `internal/database/` | MySQL store for configuration data; `internal/database/migrations/` holds versioned goose SQL migrations (see `docs/migrations.md`) |
| `internal/integration/` | In-process integration tests (report/alert scheduler, REST API) — runs via `make test` |
| `schemas/` | JSON schemas referenced by the config import/export tooling |
| `workflows/` | Portable workflow bundle JSON files, organized by metric (e.g. `code-coverage/`, `issues/`) |
| `docs/` | REST API reference, AI analytics prompt, architecture decisions, MCP server (`docs/mcp-server.md`), workflows (`docs/workflows.md`) |

## Build and test

```sh
make build          # build PatternFly frontend + Go binary (bin/orgpulse)
make frontend-build # build PatternFly frontend only (requires Node.js ≥ 24)
make test         # run tests with race detector
make vet          # go vet
make fmt-check    # verify gofmt formatting
make lint         # golangci-lint (config: .golangci.yml)
make image        # build container image (includes React build via multi-stage Containerfile)
```

## Configuration

All config via environment variables. Everything is optional (the dashboard frontend and
`/health/live`, `/health/ready`, `/metrics` work with no configuration); database,
forwarded-identity auth, and report-service env vars unlock further features. See
README.md for the full table.

## Key conventions

- Go 1.26, standard library plus vetted third-party packages (see `go.mod`) — avoid
  unnecessary new dependencies
- After changing Go, run `make lint` and fix findings. Config is `.golangci.yml`.
  Prefer fixing over `//nolint` / `#nosec` / disabling linters.
- Tests use `net/http/httptest` for mock servers
- No comments unless the "why" is non-obvious
- Database migrations: MySQL DDL implicitly commits; DDL migrations must declare `-- +goose NO TRANSACTION`. Prefer one atomic DDL statement per migration file (combine same-table changes in a single `ALTER TABLE`, split multi-table changes into separate migration files). Never use `IF EXISTS`/`IF NOT EXISTS` to mask failures, never casually rewrite deployed migrations, and follow the rules and recovery protocol in `docs/migrations.md`.

## Commit style

[Conventional Commits](https://www.conventionalcommits.org/):
`<type>(<scope>): <description>`. Types: `feat`, `fix`, `docs`, `style`, `refactor`,
`test`, `chore`.

When AI assists with a commit, include a trailer:

```
feat: add user authentication

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

**Never push to a remote or open a pull request without explicit user approval.**

## Git workflow

- The public upstream is `github.com/fullsend-ai/orgpulse`.
- After initial repository setup, work happens on a feature branch, never directly on `main`.
- Branch naming: `<type>/<short-description>` (matches commit types).
- Use GitHub pull requests for code review.

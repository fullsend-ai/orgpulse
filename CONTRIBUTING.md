# Contributing

Thanks for your interest in contributing.

## Reporting security issues

Do not open public issues for vulnerabilities. See [SECURITY.md](SECURITY.md).

## Development

Build, test, and lint commands, the repository layout, and project conventions
are described in [AGENTS.md](AGENTS.md) and [README.md](README.md). Before
opening a pull request:

```sh
make test       # Go tests with the race detector
make lint       # golangci-lint
make fmt-check  # gofmt
```

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
(`<type>(<scope>): <description>`).

Third-party notices live in [NOTICE](NOTICE). Dependencies and their versions
are recorded in `go.mod`/`go.sum` and the frontend lockfile.

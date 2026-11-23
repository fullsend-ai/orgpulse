# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately through GitHub's private
vulnerability reporting: open the repository's **Security** tab and choose
**Report a vulnerability**. Do not open a public issue, pull request, or
discussion for a suspected vulnerability.

Reports can also be sent to `secalert@redhat.com`. Do not use public issues
for either channel.

Include what you can of:

- the affected component (backend, frontend, VS Code extension, deployment
  template) and version or commit
- steps to reproduce, or a proof of concept
- the impact you expect

The maintainers will acknowledge the report, keep you informed while it is
investigated and fixed, and credit you in the advisory unless you prefer
otherwise.

## Supported versions

Security fixes are made on the `main` branch and included in the next
release. Older releases are not patched.

## Deployment hardening

Several settings weaken authentication and are meant for local development
only; the server logs a warning at startup when any is enabled:
`DEV_ADMIN_OVERRIDE`, `AUTH_TEST_MODE`, `ADMIN_USERS=*`, and
`TLS_SKIP_VERIFY`. In production:

- Serve the dashboard only through an authenticating proxy that sets
  `X-Forwarded-User` / `X-Forwarded-Email`, and make sure the application port
  cannot be reached without it. The forwarded headers are trusted as-is.
- Set `AUTH_EMAIL_DOMAINS` to your identity provider's email domain(s).
- Set `CONFIG_ENCRYPTION_KEY` so stored integration tokens are encrypted.
- Use TLS for both MySQL connections (`*_TLS_CA_BUNDLE_PATH`).

See [README.md](README.md) for every setting.

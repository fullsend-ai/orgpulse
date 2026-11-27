# e2e testing

<!-- fullsend /fs-code smoke test -->

## Why "e2e" and not "integration"

These tests start a real MySQL container using `podman` and are not included
in `make test`. They can run locally or in an environment that supports
starting containers. Integration tests run as part of `make test`.

## DB_TLS_CA_BUNDLE_PATH e2e tests

`internal/database/tls_e2e_test.go` (build tag `e2e`) proves
`DB_TLS_CA_BUNDLE_PATH` actually works against a real MySQL server enforcing
`require_secure_transport`, not just that `buildDSN` constructs the expected
string (that part is already covered by the plain unit tests in
`database_test.go`). `make test-e2e` also runs
`internal/database/migrate_e2e_test.go`'s `TestE2E_Migrate_AppliesSchemaIdempotently`,
which applies the goose schema migration (twice, to prove idempotency)
against this same TLS-enforcing container.

Run with:

```sh
make test-e2e
```

This starts `registry.redhat.io/rhel9/mysql-84` with the fixture certs below
mounted in, enables `require_secure_transport`, and runs the tests against it.
Pulling this image may require registry credentials.

## Fixture certs (`internal/database/testdata/tls/`)

MySQL's own auto-generated server certificate has no Subject Alternative
Name at all, which Go's TLS client rejects outright (Go 1.15+ requires a SAN
match, no fallback to CN) — regardless of which hostname you connect with.
Real RDS certificates *do* have a proper SAN (the actual endpoint hostname),
so this is a test-fixture problem, not a production code issue. The fix:
mount a purpose-built CA + server certificate (with an IP SAN for
`127.0.0.1`) into the test container instead of relying on MySQL's own
auto-generated one.

These are **not** checked in. `make test-e2e` runs `make e2e-tls-fixtures`
first, which generates a fresh CA and server certificate (7-day validity)
into `internal/database/testdata/tls/` (gitignored) with `openssl`. A
committed private key, even a test-only one, trips secret scanners.

The target runs the equivalent of:

```sh
cd internal/database/testdata/tls

# CA
openssl req -x509 -newkey rsa:2048 -days 7 -nodes \
  -keyout ca-key.pem -out ca.pem \
  -subj "/CN=orgpulse-test-ca"

# Server cert, signed by that CA, with an IP SAN for 127.0.0.1
openssl req -newkey rsa:2048 -nodes -keyout server-key.pem -out server-req.pem \
  -subj "/CN=127.0.0.1"
echo "subjectAltName=IP:127.0.0.1" > ext.cnf
openssl x509 -req -in server-req.pem -days 7 -CA ca.pem -CAkey ca-key.pem \
  -CAcreateserial -out server-cert.pem -extfile ext.cnf

rm -f server-req.pem ext.cnf ca.srl
```

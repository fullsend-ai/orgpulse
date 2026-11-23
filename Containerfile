FROM registry.access.redhat.com/ubi9/nodejs-24:latest@sha256:d843698c5564f9a066a9f45bb1b0392b0b0926f3aa1b37a242b99ed19f16408a AS frontend-builder
WORKDIR /opt/app-root/src
USER 0
COPY web/patternfly/package.json web/patternfly/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/patternfly/ .
RUN chown -R 1001:0 /opt/app-root/src
USER 1001
RUN npm run build

FROM registry.access.redhat.com/ubi9/go-toolset:1.26.10@sha256:0e3894cea4fc077084245e8aa2befc8a7d91a4f60fbf850f0b5b555cbe62ee26 AS builder
# /opt/app-root/src is go-toolset's default WORKDIR, owned by its default
# non-root user (1001) — build there instead of creating a new top-level
# directory, which that user can't do.
WORKDIR /opt/app-root/src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /opt/app-root/src/build web/patternfly/build/
RUN CGO_ENABLED=0 go build -buildvcs=false -o out/orgpulse ./cmd/orgpulse/
RUN CGO_ENABLED=0 go build -buildvcs=false -o out/migrate ./cmd/migrate/
RUN CGO_ENABLED=0 go build -buildvcs=false -o out/seed-test-data ./cmd/seed-test-data/
RUN CGO_ENABLED=0 go build -buildvcs=false -o out/mock-diagrams ./cmd/mock-diagrams/

# Dev-only tooling image (seed-test-data, mock-diagrams) used by compose-dev.yaml
# via `target: devtools`. Not published anywhere.
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8@sha256:f26ef64172ea61f5ac5ba4be8c5c738bf8760b22b3e5148ce7b40f7308ab32db AS devtools
COPY --from=builder /opt/app-root/src/out/seed-test-data /opt/app-root/src/out/mock-diagrams /
USER 1001

# Production image. Kept as the last stage so plain `podman build .` (no
# --target) produces this image by default.
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8@sha256:f26ef64172ea61f5ac5ba4be8c5c738bf8760b22b3e5148ce7b40f7308ab32db AS dashboard
COPY --from=builder /opt/app-root/src/out/orgpulse /orgpulse
COPY --from=builder /opt/app-root/src/out/migrate /migrate
COPY --from=builder /opt/app-root/src/LICENSE /opt/app-root/src/NOTICE /licenses/
EXPOSE 9090
USER 1001
ENTRYPOINT ["/orgpulse"]

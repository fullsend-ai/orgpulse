BINARY  := bin/orgpulse
IMAGE   := quay.io/dprod/devlake-dashboard
TAG     ?= latest
GO      := go
GOFLAGS ?=
NPM     := npm
FRONTEND_DIR := web/patternfly

PORT ?= 9090

E2E_TEST_CONTAINER := orgpulse-e2e-test
E2E_TEST_IMAGE      := registry.redhat.io/rhel9/mysql-84:latest
E2E_TLS_FIXTURES    := internal/database/testdata/tls

.PHONY: build build-mcp frontend-build frontend-dist test test-e2e e2e-tls-fixtures vet fmt-check lint clean image push query chart report create-admin-key run import-config clear-config clear-workflows seed-test-data migrate dev dev-down

frontend-build: ## Build the PatternFly frontend (requires Node.js ≥ 18)
	cd $(FRONTEND_DIR) && $(NPM) ci --ignore-scripts && $(NPM) run build

frontend-dist: ## Ensure web/patternfly/build exists (placeholder for go embed when skipping the full frontend build)
	@mkdir -p $(FRONTEND_DIR)/build
	@test -f $(FRONTEND_DIR)/build/index.html || printf '<!DOCTYPE html><html><body><p>Frontend build required — run <code>make frontend-build</code></p></body></html>\n' > $(FRONTEND_DIR)/build/index.html

build: frontend-build ## Build the PatternFly frontend and the Go binary
	$(GO) build $(GOFLAGS) -buildvcs=false -o $(BINARY) ./cmd/orgpulse/

build-mcp: ## Build the MCP server binary
	$(GO) build $(GOFLAGS) -buildvcs=false -o bin/mcp-server ./cmd/mcp-server/

test: frontend-dist ## Run all tests
	$(GO) test ./... -race -count=1


e2e-tls-fixtures: ## Generate throwaway CA + server cert (IP SAN 127.0.0.1) for test-e2e (requires openssl)
	@set -e; mkdir -p $(E2E_TLS_FIXTURES); cd $(E2E_TLS_FIXTURES); \
	openssl req -x509 -newkey rsa:2048 -days 7 -nodes -keyout ca-key.pem -out ca.pem \
		-subj "/CN=orgpulse-test-ca" 2>/dev/null; \
	openssl req -newkey rsa:2048 -nodes -keyout server-key.pem -out server-req.pem \
		-subj "/CN=127.0.0.1" 2>/dev/null; \
	echo "subjectAltName=IP:127.0.0.1" > ext.cnf; \
	openssl x509 -req -in server-req.pem -days 7 -CA ca.pem -CAkey ca-key.pem \
		-CAcreateserial -out server-cert.pem -extfile ext.cnf 2>/dev/null; \
	rm -f server-req.pem ext.cnf ca.srl; \
	chmod 0644 ca.pem ca-key.pem server-cert.pem server-key.pem

test-e2e: e2e-tls-fixtures ## Run e2e tests against a real MySQL container enforcing require_secure_transport (requires podman and openssl; not run in CI -- see docs/e2e-testing.md)
	@set -e; \
	certdir=$$(cd "$(E2E_TLS_FIXTURES)" && pwd -P); \
	printf '[mysqld]\nssl-ca=/certs/ca.pem\nssl-cert=/certs/server-cert.pem\nssl-key=/certs/server-key.pem\n' > "$$certdir/ssl.cnf"; \
	trap 'podman rm -f $(E2E_TEST_CONTAINER) >/dev/null 2>&1; rm -f "$$certdir/ssl.cnf"' EXIT; \
	echo "Starting $(E2E_TEST_IMAGE)..."; \
	podman run -d --name $(E2E_TEST_CONTAINER) \
		-e MYSQL_ROOT_PASSWORD=rootpass -e MYSQL_DATABASE=testdb \
		-e MYSQL_USER=testuser -e MYSQL_PASSWORD=testpass \
		-v "$$certdir/ca.pem:/certs/ca.pem:ro" \
		-v "$$certdir/server-cert.pem:/certs/server-cert.pem:ro" \
		-v "$$certdir/server-key.pem:/certs/server-key.pem:ro" \
		-v "$$certdir/ssl.cnf:/etc/my.cnf.d/ssl.cnf:ro" \
		-p 13306:3306 \
		$(E2E_TEST_IMAGE) >/dev/null; \
	for i in $$(seq 1 30); do \
		podman exec $(E2E_TEST_CONTAINER) mysqladmin ping -h127.0.0.1 -uroot -prootpass >/dev/null 2>&1 && break; \
		[ "$$i" -eq 30 ] && { echo "MySQL did not become ready in time"; exit 1; }; \
		sleep 2; \
	done; \
	podman exec $(E2E_TEST_CONTAINER) mysql -h127.0.0.1 -uroot -prootpass \
		-e "SET GLOBAL require_secure_transport=ON;" >/dev/null; \
	TEST_MYSQL_HOST=127.0.0.1 TEST_MYSQL_PORT=13306 TEST_MYSQL_USER=testuser TEST_MYSQL_PASSWORD=testpass \
	TEST_MYSQL_DATABASE=testdb \
	$(GO) test -tags e2e ./internal/database/... -run TestE2E -v

vet: frontend-dist ## Run go vet
	$(GO) vet ./...

fmt-check: ## Check that all files are gofmt'd
	@test -z "$$(gofmt -l .)" || { gofmt -l . ; echo "above files are not gofmt'd"; exit 1; }

lint: frontend-dist ## Run golangci-lint
	golangci-lint run

image: ## Build container image
	podman build -t $(IMAGE):$(TAG) .

push: image ## Build and push container image to quay.io
	podman push $(IMAGE):$(TAG)

health: ## Check dashboard liveness via /health/live
	@curl -sf http://localhost:$(PORT)/health/live | jq .

query: ## Query the running dashboard's endpoints
	@echo "=== /health/ready ==="
	@curl -sf http://localhost:$(PORT)/health/ready | jq .

chart: build ## Render a chart from a JSON file: make chart FILE=input.json [OUT=chart.png]
	@test -n "$(FILE)" || { echo "usage: make chart FILE=input.json [OUT=chart.png]"; exit 1; }
	@PORT=$(PORT) AUTH_TEST_MODE=true \
		./$(BINARY) &>/dev/null & pid=$$!; \
	trap "kill $$pid 2>/dev/null" EXIT; \
	for i in $$(seq 1 30); do \
		curl -sf http://localhost:$(PORT)/health/live >/dev/null 2>&1 && break; \
		sleep 0.1; \
	done; \
	curl -sf -X POST -H 'Content-Type: application/json' -d @$(FILE) \
		http://localhost:$(PORT)/chart -o $(or $(OUT),chart.png) && \
	echo "Saved to $(or $(OUT),chart.png)"

report: build ## Generate HTML report: make report FILE=input.json [OUT=report.html]
	@test -n "$(FILE)" || { echo "usage: make report FILE=input.json [OUT=report.html]"; exit 1; }
	@PORT=$(PORT) AUTH_TEST_MODE=true AUTH_TEST_IS_ADMIN=true \
		./$(BINARY) &>/dev/null & pid=$$!; \
	trap "kill $$pid 2>/dev/null" EXIT; \
	for i in $$(seq 1 30); do \
		curl -sf http://localhost:$(PORT)/health/live >/dev/null 2>&1 && break; \
		sleep 0.1; \
	done; \
	curl -sf -X POST -H 'Content-Type: application/json' -d @$(FILE) \
		http://localhost:$(PORT)/sendreport -o $(or $(OUT),report.html) && \
	echo "Saved to $(or $(OUT),report.html)"

run: build ## Build and run the dashboard locally using environment variables
	./$(BINARY)

import-config: ## Import config from DIR: make import-config DIR=path/to/data
	@test -n "$(DIR)" || { echo "usage: make import-config DIR=path/to/data"; exit 1; }
	@$(GO) run ./cmd/import-config/ $(DIR)

clear-config: ## Delete all config data from the database (preserves API keys)
	@$(GO) run ./cmd/clear-config/

create-admin-key: ## Create an admin API key in the database (requires DASHBOARD_DB_USER, DASHBOARD_DB_PASSWORD, DASHBOARD_DB_NAME)
	@$(GO) run ./cmd/create-admin-key/

migrate: ## Apply pending database migrations (requires DASHBOARD_DB_USER, DASHBOARD_DB_PASSWORD, DASHBOARD_DB_NAME)
	@$(GO) run ./cmd/migrate/

clear-workflows: ## Clear all workflows, versions, and stored procedures from the database
	@$(GO) run ./cmd/clear-workflows/

seed-test-data: ## Seed the database with test data for manual exploration
	@$(GO) run ./cmd/seed-test-data/

dev: ## Full local dev environment with mock data: MySQL + mock-diagrams + seeded dashboard
	podman compose -f compose-dev.yaml up --build

dev-down: ## Tear down the local dev environment started by `make dev`
	podman compose -f compose-dev.yaml down

clean: ## Remove build artifacts
	rm -rf bin/ $(FRONTEND_DIR)/build $(FRONTEND_DIR)/node_modules

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

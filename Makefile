.PHONY: help dev build run test clean migrate-up migrate-down migrate-create docker-up docker-down docker-logs seed seedcbb seed-test-data sync-calendar tools vendor-htmx fmt fmt-check fix-check vet vulncheck

# Default target
help:
	@echo "Available commands:"
	@echo "  make dev            - Start development environment with hot reload"
	@echo "  make build          - Build the production binary"
	@echo "  make run            - Run the server locally (requires local DB)"
	@echo "  make test           - Run all tests"
	@echo "  make vet            - Run go vet"
	@echo "  make fmt-check      - Verify gofmt formatting"
	@echo "  make fix-check      - Verify go fix has no pending modernizations"
	@echo "  make vulncheck      - Scan dependencies for vulnerabilities"
	@echo "  make vendor-htmx    - Re-download the vendored htmx build and verify its checksum"
	@echo "  make clean          - Remove build artifacts"
	@echo "  make migrate-up     - Run database migrations (up)"
	@echo "  make migrate-down   - Rollback last migration"
	@echo "  make migrate-create - Create a new migration (usage: make migrate-create name=migration_name)"
	@echo "  make docker-up      - Start Docker containers"
	@echo "  make docker-down    - Stop Docker containers"
	@echo "  make docker-logs    - View Docker logs"
	@echo "  make docker-build   - Build Docker images"
	@echo "  make seed           - Seed database with CFB data (usage: make seed year=2024 week=1 seasonType=regular)"
	@echo "  make seedcbb        - Seed database with CBB data (usage: make seedcbb season=2025)"
	@echo "  make seed-test-data - Add test users, leagues, and a mix of bets against seeded games"
	@echo "  make sync-calendar  - Sync calendar data for all years (2002 - present)"

# Development with hot reload using Docker
dev:
	docker compose up --build

# Build production binary
build:
	CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/server ./cmd/server

# Run locally (requires DATABASE_URL to be set)
run:
	go run ./cmd/server

# Run tests
test:
	go test -v -race -cover ./...

# Clean build artifacts
clean:
	rm -rf bin/ tmp/
	go clean

# Database migrations - Up
migrate-up:
	docker compose run --rm migrate up

# Database migrations - Down (rollback one)
migrate-down:
	docker compose run --rm migrate down 1

# Create new migration
# Usage: make migrate-create name=create_leagues
migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "Error: Please provide a migration name. Usage: make migrate-create name=migration_name"; \
		exit 1; \
	fi
	@mkdir -p migrations
	@TIMESTAMP=$$(date +%Y%m%d%H%M%S); \
	touch migrations/$${TIMESTAMP}_$(name).up.sql; \
	touch migrations/$${TIMESTAMP}_$(name).down.sql; \
	echo "Created migrations/$${TIMESTAMP}_$(name).up.sql"; \
	echo "Created migrations/$${TIMESTAMP}_$(name).down.sql"

# Docker commands
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-build:
	docker compose build

# Database only (useful for local development without Docker app)
db-up:
	docker compose up -d db

db-down:
	docker compose down db

# Install development tools pinned by the go.mod `tool` directive.
tools:
	go install tool

# Vendored frontend dependencies. htmx is served out of static/ rather than
# loaded from a CDN so the app carries no third-party runtime dependency.
# To upgrade, bump both variables together, then run `make vendor-htmx`.
HTMX_VERSION := 2.0.10
HTMX_SHA256  := 71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de

vendor-htmx:
	@mkdir -p static/js
	@tmp=$$(mktemp); \
	if curl -sSfL -o "$$tmp" https://unpkg.com/htmx.org@$(HTMX_VERSION)/dist/htmx.min.js \
		&& echo "$(HTMX_SHA256)  $$tmp" | sha256sum -c --status -; then \
		mv "$$tmp" static/js/htmx.min.js; \
		chmod 644 static/js/htmx.min.js; \
		echo "vendored htmx $(HTMX_VERSION)"; \
	else \
		rm -f "$$tmp"; \
		echo "failed to vendor htmx $(HTMX_VERSION): download failed or checksum mismatch"; \
		exit 1; \
	fi

# Format and lint
fmt:
	go fmt ./...

vet:
	go vet ./...

# Verify all files are gofmt-clean (used by CI).
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "These files are not gofmt-formatted:"; echo "$$out"; exit 1; \
	fi

# Verify go fix has no pending modernizations (used by CI).
fix-check:
	@out=$$(go fix -diff ./...); \
	if [ -n "$$out" ]; then \
		echo "go fix found improvements:"; echo "$$out"; exit 1; \
	fi

# Scan dependencies for known vulnerabilities.
vulncheck:
	go tool govulncheck ./...

# Seed database with CFB data.
# Usage: make seed year=2024 week=1 seasonType=regular
seed:
	@CMD="go run ./cmd/seed"; \
	if [ -n "$(year)" ]; then CMD="$$CMD -year=$(year)"; fi; \
	if [ -n "$(week)" ]; then CMD="$$CMD -week=$(week)"; fi; \
	if [ -n "$(seasonType)" ]; then CMD="$$CMD -seasonType=$(seasonType)"; fi; \
	$$CMD

# Seed database with CBB data.
# Usage: make seedcbb season=2025
seedcbb:
	@CMD="go run ./cmd/seedcbb"; \
	if [ -n "$(season)" ]; then CMD="$$CMD -season=$(season)"; fi; \
	$$CMD

# Add test users, test leagues, and a mix of pending/won/lost bets against
# whatever games are already in the database (run `make seed` first).
seed-test-data:
	go run ./cmd/seedtestdata

# Sync calendar data for all years.
sync-calendar:
	go run ./cmd/synccalendar

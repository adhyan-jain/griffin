.PHONY: build run test test-v fmt lint setup-hooks migrate migrate-status migrate-down package clean help

# Default target
all: build

## Build the Griffin server binary
build:
	go build -o bin/griffin-server ./cmd/griffin-server

## Run the server locally
run: build
	set -a; . ./.env; set +a; ./bin/griffin-server

## Run all automated unit and integration tests
test:
	go test ./internal/... ./cmd/... ./tests/...

## Run all automated tests with verbose output
test-v:
	go test -v ./internal/... ./cmd/... ./tests/...

## Format Go source files
fmt:
	gofmt -w -s .

## Run static analysis and linting
lint:
	go vet ./internal/... ./cmd/... ./tests/...

## Configure git pre-commit hooks
setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "Git hooks configured to .githooks/"

## Apply migrations explicitly. LOCAL plain PostgreSQL only (creates a minimal auth shim).
## Requires DATABASE_URL (or MIGRATION_DATABASE_URL). For Supabase run: go run ./cmd/griffin-migrate up
migrate:
	go run ./cmd/griffin-migrate -local-auth-shim up

## Show applied/pending migrations
migrate-status:
	go run ./cmd/griffin-migrate status

## Roll back local migrations manually (destructive; local dev databases only)
migrate-down:
	@echo "Down migrations are manual and destructive: apply migrations/*.down.sql in reverse order with psql against a LOCAL database."

## Build offline deployment bundles (Lambda zip + EC2 worker tarball) into build/
package:
	bash scripts/package-lambda.sh
	bash scripts/package-worker.sh

## Clean build artifacts
clean:
	rm -rf bin/

## Display available make commands
help:
	@echo "Available commands:"
	@echo "  make build        - Compile server binary into bin/griffin-server"
	@echo "  make run          - Build and execute griffin-server"
	@echo "  make test         - Run full test suite"
	@echo "  make test-v       - Run full test suite with verbose output"
	@echo "  make fmt          - Format Go files using gofmt"
	@echo "  make lint         - Run static analysis with go vet"
	@echo "  make setup-hooks  - Configure git pre-commit hooks"
	@echo "  make migrate      - Apply SQL migrations to a LOCAL Postgres (with auth shim)"
	@echo "  make package      - Build Lambda zip and EC2 worker bundle (offline)"
	@echo "  make migrate-down - Rollback migrations on local griffin_dev db"
	@echo "  make clean        - Remove compiled binaries"

.PHONY: build run test test-v fmt lint setup-hooks migrate migrate-down clean help

# Default target
all: build

## Build the Griffin server binary
build:
	go build -o bin/griffin-server ./cmd/griffin-server

## Run the server locally
run: build
	./bin/griffin-server

## Run all automated unit and integration tests
test:
	go test ./...

## Run all automated tests with verbose output
test-v:
	go test -v ./...

## Format Go source files
fmt:
	gofmt -w -s .

## Run static analysis and linting
lint:
	go vet ./...

## Configure git pre-commit hooks
setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "Git hooks configured to .githooks/"

## Apply database migrations using psql to griffin_dev
migrate:
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000001_init_schema.up.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000002_rls_policies.up.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000003_orchestration_and_agent_factory.up.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000004_s3_storage_artifacts.up.sql

## Roll back database migrations on griffin_dev
migrate-down:
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000004_s3_storage_artifacts.down.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000003_orchestration_and_agent_factory.down.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000002_rls_policies.down.sql
	PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d griffin_dev -f migrations/000001_init_schema.down.sql

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
	@echo "  make migrate      - Apply SQL migrations to local griffin_dev db"
	@echo "  make migrate-down - Rollback migrations on local griffin_dev db"
	@echo "  make clean        - Remove compiled binaries"

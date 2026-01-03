.PHONY: help build test lint clean package terraform-fmt terraform-validate deploy-dev deploy-prod local-up local-test local-down

# Variables
GO := go
GOFMT := gofmt
GOLINT := golangci-lint
TERRAFORM := terraform
DIST_DIR := dist
LAMBDA_DIRS := cmd/ingest cmd/processor cmd/query

# Default target
help:
	@echo "Available targets:"
	@echo "  build            - Build all Lambda functions"
	@echo "  test             - Run all tests"
	@echo "  lint             - Run linters (golangci-lint)"
	@echo "  clean            - Remove build artifacts"
	@echo "  package          - Package Lambda functions as ZIP files"
	@echo "  terraform-fmt    - Format Terraform files"
	@echo "  terraform-validate - Validate Terraform configuration"
	@echo "  deploy-dev       - Deploy to dev environment"
	@echo "  deploy-prod      - Deploy to prod environment"
	@echo "  verify-dev       - Verify dev deployment end-to-end (requires terraform apply + migrations)"
	@echo "  local-up         - Start local PostgreSQL with docker-compose"
	@echo "  local-test       - Run local test harness (requires local-up)"
	@echo "  local-down       - Stop local PostgreSQL"

# Build Lambda functions
build:
	@echo "Building Lambda functions..."
	@mkdir -p $(DIST_DIR)
	@for dir in $(LAMBDA_DIRS); do \
		echo "Building $$dir..."; \
		cd $$dir && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -tags lambda.norpc -o bootstrap main.go && \
		mv bootstrap ../../$(DIST_DIR)/$$(basename $$dir) && \
		cd ../..; \
	done
	@echo "Build complete"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Test coverage report: coverage.html"

# Lint code
lint:
	@echo "Running linters..."
	@if ! command -v $(GOLINT) > /dev/null; then \
		echo "golangci-lint not found. Installing..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.55.2; \
	fi
	$(GOLINT) run ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(DIST_DIR)
	rm -f coverage.out coverage.html
	find . -name "*.zip" -type f -delete
	@echo "Clean complete"

# Package Lambda functions as ZIP files
package: build
	@echo "Packaging Lambda functions..."
	@for dir in $(LAMBDA_DIRS); do \
		name=$$(basename $$dir); \
		echo "Packaging $$name..."; \
		cd $(DIST_DIR) && zip -r $$name.zip $$name > /dev/null && rm -rf $$name && cd ..; \
	done
	@echo "Packaging complete"

# Format Terraform files
terraform-fmt:
	@echo "Formatting Terraform files..."
	$(TERRAFORM) fmt -recursive infra/terraform/
	@echo "Formatting complete"

# Validate Terraform configuration
terraform-validate:
	@echo "Validating Terraform configuration..."
	@for env in dev prod; do \
		echo "Validating $$env environment..."; \
		cd infra/terraform/envs/$$env && \
		$(TERRAFORM) init -backend=false > /dev/null && \
		$(TERRAFORM) validate && \
		cd ../../..; \
	done
	@echo "Validation complete"

# Deploy to dev environment
deploy-dev:
	@echo "Deploying to dev environment..."
	@echo "1. Building and packaging..."
	$(MAKE) package
	@echo "2. Initializing Terraform..."
	cd infra/terraform/envs/dev && $(TERRAFORM) init
	@echo "3. Planning deployment..."
	cd infra/terraform/envs/dev && $(TERRAFORM) plan
	@echo "4. Apply when ready: cd infra/terraform/envs/dev && terraform apply"

# Deploy to prod environment
deploy-prod:
	@echo "Deploying to prod environment..."
	@echo "1. Building and packaging..."
	$(MAKE) package
	@echo "2. Initializing Terraform..."
	cd infra/terraform/envs/prod && $(TERRAFORM) init
	@echo "3. Planning deployment..."
	cd infra/terraform/envs/prod && $(TERRAFORM) plan
	@echo "4. Apply when ready: cd infra/terraform/envs/prod && terraform apply"

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Run all checks (for CI)
ci: deps lint test terraform-fmt terraform-validate

# Local development targets
local-up:
	@echo "Starting local PostgreSQL..."
	cd local && docker-compose up -d
	@echo "Waiting for PostgreSQL to be ready..."
	@timeout 30 sh -c 'until docker exec fluxa-postgres-local pg_isready -U fluxa_user -d fluxa > /dev/null 2>&1; do sleep 1; done' || echo "PostgreSQL ready"
	@echo "PostgreSQL is ready"

local-test: local-up
	@echo "Running local test harness..."
	@cd local && go run main.go

local-down:
	@echo "Stopping local PostgreSQL..."
	cd local && docker-compose down

# Verify dev deployment
verify-dev:
	@echo "Verifying dev deployment..."
	./scripts/verify_dev.sh


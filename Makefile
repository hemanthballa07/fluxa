.PHONY: help up down build logs test lint clean replay ps proto proto-tools grpc-tools k6-fraud

# Default target
help:
	@echo "Fluxa — Local Fraud Detection Platform"
	@echo ""
	@echo "Available targets:"
	@echo "  up        - Build and start all services (except replay)"
	@echo "  down      - Stop and remove all containers"
	@echo "  build     - Build all service Docker images"
	@echo "  logs      - Follow logs for all running services"
	@echo "  ps        - Show status of all containers"
	@echo "  replay    - Start the dataset replay service (requires ./data/transactions.csv)"
	@echo "  test      - Run all Go tests"
	@echo "  lint      - Run golangci-lint"
	@echo "  clean     - Remove build artifacts and stop containers"
	@echo ""
	@echo "Quick start:"
	@echo "  1. cp ~/Downloads/transactions.csv ./data/"
	@echo "  2. make up"
	@echo "  3. make replay"
	@echo "  4. open http://localhost:3000  (Grafana, admin/admin)"

# Start full stack (infrastructure + services, not replay)
up:
	@mkdir -p data
	docker compose up -d --build
	@echo ""
	@echo "Services ready:"
	@echo "  Ingest:    http://localhost:8080"
	@echo "  Query:     http://localhost:8083"
	@echo "  RabbitMQ:  http://localhost:15672  (fluxa/fluxa_pass)"
	@echo "  MinIO:     http://localhost:9001   (minioadmin/minioadmin123)"
	@echo "  Prometheus:http://localhost:9090"
	@echo "  Grafana:   http://localhost:3000   (admin/admin)"

# Stop all containers
down:
	docker compose --profile replay down

# Build images without starting
build:
	docker compose build --parallel

# Follow logs
logs:
	docker compose logs -f

# Show container status
ps:
	docker compose ps

# Start replay service (CSV must be at ./data/transactions.csv)
replay:
	@if [ ! -f data/transactions.csv ]; then \
		echo "ERROR: ./data/transactions.csv not found."; \
		echo "Download PaySim from https://www.kaggle.com/datasets/ealaxi/paysim1"; \
		exit 1; \
	fi
	docker compose --profile replay up -d replay
	docker compose logs -f replay

# Run Go tests (requires local PostgreSQL via 'make up')
test:
	go test -v -race ./...

# Run linter
lint:
	@if ! command -v golangci-lint > /dev/null; then \
		echo "golangci-lint not found. Install via: brew install golangci-lint"; \
		exit 1; \
	fi
	golangci-lint run ./...

# Clean up
clean:
	docker compose --profile replay down -v
	rm -f coverage.out coverage.html

# Install protoc Go plugins (one-time setup before `make proto`).
# Versions pinned for Go 1.22 compatibility — protoc-gen-go@latest currently
# requires Go 1.23. Bump these when go.mod moves to 1.23+.
proto-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.2
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# Install grpcurl for ad-hoc gRPC smoke testing (macOS via Homebrew; idempotent)
grpc-tools:
	@command -v grpcurl >/dev/null || brew install grpcurl

# Generate Go stubs from proto files (requires `make proto-tools` first)
proto:
	@command -v protoc-gen-go-grpc >/dev/null || (echo 'run `make proto-tools` first' && exit 1)
	protoc --go_out=. --go_opt=module=github.com/fluxa/fluxa \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/fluxa/fluxa \
	       proto/fraud/v1/fraud_eval.proto
	protoc --go_out=. --go_opt=module=github.com/fluxa/fluxa \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/fluxa/fluxa \
	       proto/scorer/v1/scorer.proto

# Run k6 SLO check against fraud-grpc (requires service up via `make up`)
k6-fraud:
	k6 run scripts/k6/fraud_grpc_p99.js

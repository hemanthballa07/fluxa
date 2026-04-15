.PHONY: help up down build logs test lint clean replay ps

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

.PHONY: help build run test clean lint dev dev-down docker-up docker-down docker-logs fmt db-seed

help:
	@echo "Student Job Portal Backend - Makefile Commands"
	@echo ""
	@echo "Development:"
	@echo "  make dev              - Run the application locally"
	@echo "  make dev-down         - Stop local development server"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up        - Start Docker containers"
	@echo "  make docker-down      - Stop Docker containers"
	@echo "  make docker-logs      - View Docker logs"
	@echo "  make docker-rebuild   - Rebuild Docker images"
	@echo ""
	@echo "Code:"
	@echo "  make fmt              - Format code"
	@echo "  make lint             - Run linter"
	@echo "  make test             - Run tests"
	@echo "  make db-seed          - Seed development users and portal data"
	@echo ""
	@echo "Build:"
	@echo "  make build            - Build the application"
	@echo "  make clean            - Clean build artifacts"
	@echo ""

# Development
dev:
	@echo "Starting development environment..."
	docker-compose up -d postgres minio
	@echo "Waiting for services to be ready..."
	@sleep 5
	@echo "Building and running API..."
	go run ./cmd/api/main.go

dev-down:
	@echo "Stopping development environment..."
	docker-compose down

# Docker
docker-up:
	@echo "Starting Docker containers..."
	docker-compose up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@docker-compose ps

docker-down:
	@echo "Stopping Docker containers..."
	docker-compose down

docker-rebuild:
	@echo "Rebuilding Docker images..."
	docker-compose down
	docker-compose up -d --build

docker-logs:
	docker-compose logs -f api

# Code
fmt:
	@echo "Formatting code..."
	go fmt ./...

lint:
	@echo "Running linter..."
	golangci-lint run ./...

test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Build
build:
	@echo "Building application..."
	go build -o bin/api ./cmd/api

clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f coverage.out coverage.html

# Dependencies
mod-download:
	go mod download

mod-tidy:
	go mod tidy

deps: mod-download mod-tidy

# Database
db-migrate-up:
	@echo "Running migrations..."
	# migrate -path migrations -database "$$DATABASE_URL" up

db-migrate-down:
	@echo "Rolling back migrations..."
	# migrate -path migrations -database "$$DATABASE_URL" down

db-seed:
	@echo "Seeding database..."
	go run ./cmd/seed

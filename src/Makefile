.PHONY: run build test lint \
	migrate-up migrate-down migrate-create \
	docker-up docker-down docker-logs docker-rebuild

BINARY      := bin/api
PKG         := ./...
MIGRATIONS  := ./migrations

# DSN для golang-migrate. Берётся из переменных окружения, см. .env.example.
POSTGRES_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=$(POSTGRES_SSL_MODE)

# --- Application ---

run:
	go run ./cmd/api

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/api

test:
	go test -race -count=1 $(PKG)

lint:
	go vet $(PKG)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, skipping"

# --- Migrations (golang-migrate) ---

migrate-up:
	migrate -path $(MIGRATIONS) -database "$(POSTGRES_DSN)" up

migrate-down:
	migrate -path $(MIGRATIONS) -database "$(POSTGRES_DSN)" down 1

# usage: make migrate-create name=create_feedbacks_table
migrate-create:
	migrate create -ext sql -dir $(MIGRATIONS) -seq $(name)

# --- Docker ---

docker-up:
	docker compose -f docker/docker-compose.yml up -d

docker-down:
	docker compose -f docker/docker-compose.yml down

docker-logs:
	docker compose -f docker/docker-compose.yml logs -f

docker-rebuild:
	docker compose -f docker/docker-compose.yml up -d --build --force-recreate

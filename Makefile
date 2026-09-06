# OfferLog monorepo Makefile
.PHONY: all dev api worker test lint build frontend-backend compose-up compose-down local-up local-down local-logs local-clean test-integration e2e-docker migrate-create backrestore docker-build smoke e2e help
SHELL := /bin/bash

help:
	@echo "offerlog targets:"
	@echo "  dev            run local postgres + api (dev ports)"
	@echo "  api            build & run api binary"
	@echo "  worker         build & run worker binary"
	@echo "  admin          create-user via CLI"
	@echo "  test           Go unit + integration tests"
	@echo "  frontend       build the React app into frontend/dist"
	@echo "  build          full backend + frontend build"
	@echo "  compose-up     start the self-contained deployment (Docker Compose)"
	@echo "  local-up       one-command local deployment on :8080 (Docker, no TLS)"
	@echo "  test-integration  Go unit + integration tests against a Postgres container"
	@echo "  e2e-docker     full stack + Playwright E2E, all in Docker"
	@echo "  smoke / e2e    Playwright acceptance"
	@echo "  fmt / lint     gofmt + vet + tsc"

fmt:
	cd backend && gofmt -w . && go vet ./...
	cd frontend && npx tsc --noEmit

test:
	cd backend && go test ./...

admin:
	cd backend && go run ./cmd/admin create-user -email "$(EMAIL)" -password "$(PASSWORD)"

api:
	cd backend && go run ./cmd/api

worker:
	cd backend && go run ./cmd/worker

frontend:
	cd frontend && npm ci && npm run build

build: frontend
	cd backend && go build -o ../bin/api ./cmd/api && go build -o ../bin/worker ./cmd/worker

compose-up:
	docker compose -f deploy/compose.yaml up -d --build

compose-down:
	docker compose -f deploy/compose.yaml down

# one-command local deployment: SPA+API on http://localhost:8080 (no Caddy),
# auto-creates the first account (me@example.com / testpass12345 by default,
# override with LOCAL_ADMIN_EMAIL / LOCAL_ADMIN_PASSWORD)
local-up:
	docker compose -f deploy/compose.local.yaml up -d --build
	@echo "OfferLog: http://localhost:8080  (login: $${LOCAL_ADMIN_EMAIL:-me@example.com})"

local-down:
	docker compose -f deploy/compose.local.yaml down

local-logs:
	docker compose -f deploy/compose.local.yaml logs -f api worker

local-clean:
	docker compose -f deploy/compose.local.yaml down -v --remove-orphans

# dockerized tests (deploy/compose.test.yaml): ephemeral tmpfs postgres
test-integration:
	@docker compose -f deploy/compose.test.yaml --profile integration run --rm integration; 	status=$$?; 	docker compose -f deploy/compose.test.yaml --profile integration down -v --remove-orphans >/dev/null 2>&1; 	exit $$status

e2e-docker:
	@docker compose -f deploy/compose.test.yaml --profile e2e run --rm e2e; 	status=$$?; 	docker compose -f deploy/compose.test.yaml --profile e2e down -v --remove-orphans >/dev/null 2>&1; 	exit $$status

smoke:
	cd scripts && node smoke.cjs

e2e:
	cd scripts && node e2e.cjs

# keep the two migration copies identical (go:embed is package-local)
migrations-sync:
	@cmp -s backend/db/migrations/00001_init.sql backend/internal/platform/migrate/migrations/00001_init.sql \
	  && echo "migrations in sync" \
	  || echo "WARNING: db/migrations and internal/platform/migrate/migrations differ"

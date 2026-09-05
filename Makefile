# OfferLogs monorepo Makefile
.PHONY: all dev api worker test lint build frontend-backend compose-up compose-down migrate-create backrestore docker-build smoke e2e help
SHELL := /bin/bash

help:
	@echo "offerlogs targets:"
	@echo "  dev            run local postgres + api (dev ports)"
	@echo "  api            build & run api binary"
	@echo "  worker         build & run worker binary"
	@echo "  admin          create-user via CLI"
	@echo "  test           Go unit + integration tests"
	@echo "  frontend       build the React app into frontend/dist"
	@echo "  build          full backend + frontend build"
	@echo "  compose-up     start the self-contained deployment (Docker Compose)"
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

smoke:
	cd scripts && node smoke.cjs

e2e:
	cd scripts && node e2e.cjs

# keep the two migration copies identical (go:embed is package-local)
migrations-sync:
	@cmp -s backend/db/migrations/00001_init.sql backend/internal/platform/migrate/migrations/00001_init.sql \
	  && echo "migrations in sync" \
	  || echo "WARNING: db/migrations and internal/platform/migrate/migrations differ"

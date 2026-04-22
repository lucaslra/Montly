.PHONY: setup dev-backend dev-frontend build up up-postgres down clean test

# First-time setup: install all dependencies
setup:
	cd backend && go mod tidy
	cd frontend && npm install

# Run backend in dev mode (serves API only; frontend runs separately via Vite)
dev-backend:
	cd backend && go run .

# Run frontend dev server (proxies /api to localhost:8080)
dev-frontend:
	cd frontend && npm run dev

# Build and start via Docker Compose
build:
	docker compose build

up:
	docker compose up -d

up-postgres:
	docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d

down:
	docker compose down

# Run all tests (backend + frontend)
test:
	cd backend && go test ./...
	cd frontend && npm test

# Remove containers AND the data volume
clean:
	docker compose down -v

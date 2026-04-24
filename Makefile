.PHONY: setup dev-backend dev-frontend build up up-postgres down clean test e2e e2e-headed

# First-time setup: install all dependencies
setup:
	cd backend && go mod tidy
	cd frontend && npm install
	cd e2e && npm install

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
	docker compose up -d --build

up-postgres:
	docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d

down:
	docker compose down

# Run all tests (backend + frontend)
test:
	cd backend && go test ./...
	cd frontend && npm test

# Run E2E tests (builds app image, spins up a fresh DB in Docker, runs Playwright)
e2e:
	docker compose -f docker-compose.e2e.yml up --build --abort-on-container-exit --exit-code-from playwright; \
	ret=$$?; \
	docker compose -f docker-compose.e2e.yml down -v; \
	exit $$ret

# Run E2E tests headed (app in Docker, Playwright runs locally with a visible browser)
# Requires: Node.js installed + browser binaries (run `cd e2e && npx playwright install chromium` once)
e2e-headed:
	docker compose -f docker-compose.e2e.yml up -d --build --wait app
	cd e2e && BASE_URL=http://localhost:18080 npx playwright test --headed; \
	ret=$$?; \
	docker compose -f $(CURDIR)/docker-compose.e2e.yml down -v; \
	exit $$ret

# Remove containers AND the data volume
clean:
	docker compose down -v

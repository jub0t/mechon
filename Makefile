# Common tasks. `make build` produces bin/mechon with the web UI embedded.

DATABASE_URL ?= postgres:///mechon_dev?host=/tmp
TEST_DATABASE_URL ?= postgres:///mechon_test?host=/tmp

.PHONY: build web panel dev-panel dev-web seed test generate

build: web panel

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

panel:
	CGO_ENABLED=0 go build -trimpath -o bin/mechon ./cmd/mechon

# Two terminals for development: the API on :8080 and Vite on :5173 (which proxies /api).
dev-panel:
	MECHON_DATABASE_URL="$(DATABASE_URL)" MECHON_PUBLIC_URL=http://localhost:5173 go run ./cmd/mechon serve

dev-web:
	cd web && pnpm dev

# Local dev admin. Development only: never use these on a real install.
DEV_ADMIN_EMAIL ?= admin@mechon.test
DEV_ADMIN_PASSWORD ?= dev-password-123

seed:
	@echo "$(DEV_ADMIN_PASSWORD)" | MECHON_DATABASE_URL="$(DATABASE_URL)" MECHON_PUBLIC_URL=http://localhost:5173 \
		go run ./cmd/mechon init --email $(DEV_ADMIN_EMAIL) --name "Dev Admin" --password-stdin \
		|| echo "(an admin already exists in $(DATABASE_URL); sign in with $(DEV_ADMIN_EMAIL) / $(DEV_ADMIN_PASSWORD))"

# Integration tests wipe and reuse TEST_DATABASE_URL.
test:
	MECHON_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...
	cd web && pnpm exec tsc -b

generate:
	sqlc generate

GO ?= go
NPM ?= npm
CONFIG ?= configs/config.yaml
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w -X github.com/junler/intent-admin/internal/bootstrap.Version=$(VERSION) -X github.com/junler/intent-admin/internal/bootstrap.Commit=$(COMMIT) -X github.com/junler/intent-admin/internal/bootstrap.BuildTime=$(BUILD_TIME)

.DEFAULT_GOAL := help
.PHONY: help install test test-web lint build build-web build-backend dev dev-api dev-backend dev-web migrate migrate-up migrate-down migrate-status seed serve docker release rename clean

help:
	@printf '%s\n' 'make install        Install Go and Ant Design Pro dependencies' 'make migrate-up     Apply database migrations and seed data' 'make dev            Start API :8080 and Umi Max :5173' 'make dev-backend    Start the API only' 'make dev-web        Start the frontend only' 'make test           Run Go tests' 'make test-web       Run frontend tests' 'make lint           Run go vet and frontend type checking' 'make build          Build frontend and embedded single binary' 'make docker         Build the application image' 'make release        Build snapshot release archives'

install:
	$(GO) mod download
	$(NPM) --prefix web ci

test:
	$(GO) test ./...

test-web:
	$(NPM) --prefix web test

lint:
	$(GO) vet ./...
	$(NPM) --prefix web run typecheck

build: build-web
	$(MAKE) build-backend

build-web:
	$(NPM) --prefix web run build
	mkdir -p internal/web/dist
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/web/dist/

build-backend:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/intent-admin .

dev: build-backend
	sh scripts/dev.sh "$(CONFIG)"

dev-api dev-backend:
	$(GO) run . serve --config "$(CONFIG)"

dev-web:
	$(NPM) --prefix web run dev

migrate migrate-up:
	$(GO) run . migrate up --config "$(CONFIG)"

migrate-down:
	$(GO) run . migrate down --config "$(CONFIG)"

migrate-status:
	$(GO) run . migrate status --config "$(CONFIG)"

seed:
	$(GO) run . migrate seed --config "$(CONFIG)"

serve:
	./bin/intent-admin serve --config "$(CONFIG)"

docker:
	docker build --build-arg VERSION="$(VERSION)" --build-arg COMMIT="$(COMMIT)" --build-arg BUILD_TIME="$(BUILD_TIME)" -t intent-admin:local .

release:
	goreleaser release --snapshot --clean

# Rebrand the template for a new project, e.g.
#   make rename MODULE=github.com/acme/orders NAME=Orders
rename:
	@test -n "$(MODULE)" || (echo 'MODULE is required, e.g. make rename MODULE=github.com/acme/orders NAME=Orders'; exit 2)
	@test -n "$(NAME)" || (echo 'NAME is required, e.g. make rename MODULE=github.com/acme/orders NAME=Orders'; exit 2)
	scripts/rename.sh --module "$(MODULE)" --name "$(NAME)" $(if $(BINARY),--binary "$(BINARY)",)

clean:
	rm -rf bin web/dist
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete

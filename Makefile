GO ?= go
PNPM ?= pnpm
CONFIG ?= configs/config.yaml

.DEFAULT_GOAL := help
.PHONY: help install test build build-web build-backend dev dev-api migrate serve

help:
	@printf '%s\n' 'make install       安装 Go 与前端依赖' 'make migrate       执行数据库迁移（含演示数据）' 'make dev           启动后端 :8080 与前端 :5173' 'make dev-api       仅启动后端' 'make test          运行 Go 测试' 'make build         构建前端并嵌入 Go 二进制' 'make serve         运行已构建的单二进制'

install:
	$(GO) mod download
	$(PNPM) --dir web install --frozen-lockfile

test:
	$(GO) test ./...

build: build-web
	$(MAKE) build-backend

build-web:
	$(PNPM) --dir web build
	mkdir -p internal/web/dist
	cp -R web/dist/. internal/web/dist/

build-backend:
	mkdir -p bin
	$(GO) build -o bin/intent-admin .

dev: build-backend
	sh scripts/dev.sh "$(CONFIG)"

dev-api:
	$(GO) run . serve --config "$(CONFIG)"

migrate:
	$(GO) run . migrate up --config "$(CONFIG)"

serve:
	./bin/intent-admin serve --config "$(CONFIG)"

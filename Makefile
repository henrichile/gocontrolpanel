SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PHP_VERSIONS ?= 8.3 8.4

.DEFAULT_GOAL := help

.PHONY: help
help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS=":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: web
web: ## Compila la SPA en web/dist
	cd web && npm install --no-audit --no-fund && npm run build

.PHONY: build
build: web ## Compila el binario del panel (incluye la SPA)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gocpd ./cmd/gocpd
	@echo "→ bin/gocpd $(VERSION)"

.PHONY: build-go
build-go: ## Compila solo el binario, sin recompilar la SPA
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gocpd ./cmd/gocpd

.PHONY: run
run: ## Arranca el panel en modo desarrollo
	GOCP_ENV=development go run ./cmd/gocpd

.PHONY: dev-web
dev-web: ## Arranca Vite con proxy hacia el panel en :8080
	cd web && npm run dev

.PHONY: test
test: ## Ejecuta los tests de Go
	go test ./... -count=1

.PHONY: vet
vet: ## Análisis estático
	go vet ./...

.PHONY: lint
lint: vet ## vet + comprobación de tipos del frontend
	cd web && npx tsc --noEmit

.PHONY: migrate
migrate: ## Aplica las migraciones de la base de datos
	go run ./cmd/gocpd migrate

.PHONY: createadmin
createadmin: ## Crea o repone el usuario administrador
	go run ./cmd/gocpd createadmin

.PHONY: images
images: ## Construye las imágenes FrankenPHP de los sitios
	@for v in $(PHP_VERSIONS); do \
		echo "→ gocp/frankenphp:php$$v"; \
		docker build --build-arg PHP_VERSION=$$v \
			-t gocp/frankenphp:php$$v deploy/site-image; \
	done

.PHONY: up
up: ## Levanta toda la plataforma con docker compose
	docker compose up -d --build

.PHONY: down
down: ## Detiene la plataforma (conserva los volúmenes)
	docker compose down

.PHONY: logs
logs: ## Sigue los logs del panel
	docker compose logs -f panel

.PHONY: clean
clean: ## Borra artefactos de compilación
	rm -rf bin web/dist/assets

.PHONY: clean-zone
clean-zone: ## Borra los ficheros :Zone.Identifier que deja Windows
	@bash scripts/clean-zone-identifier.sh .

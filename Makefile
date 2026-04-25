## Makefile for local development

.PHONY: all generate build compose-up compose-down smoke docs lint serve-docs

all: build

generate:
	@bash scripts/generate.sh

build:
	@go build ./...
compose-up:
	docker compose -f deployments/docker-compose.yaml up -d
compose-build-up:
	@cd deployments && docker compose build && docker compose up -d

compose-down:
	docker compose -f deployments/docker-compose.yaml down --remove-orphans
up:
	make compose-build-up
# 	docker build -t goload-frontend ./public
# 	docker run --name goloadfrontend -d -p 8081:80 goload-frontend 

smoke:
	@echo "Waiting for apigateway to be available..."
	@bash -c 'for i in {1..30}; do if curl -sSf http://localhost:8080/health >/dev/null 2>&1; then echo "apigateway ready"; exit 0; fi; sleep 1; done; echo "apigateway did not become ready"; exit 1'

OPENAPI=api/openapi.yaml
BUNDLED=docs/openapi.yaml

docs:
	redocly bundle $(OPENAPI) -o $(BUNDLED)

lint:
	redocly lint $(OPENAPI)

serve-docs:
	redocly preview-docs $(OPENAPI)

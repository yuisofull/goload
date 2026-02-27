## Makefile for local development

.PHONY: all generate build compose-up compose-down smoke

all: build

generate:
	@bash scripts/generate.sh

build:
	@go build ./...
compose-up-svc:
	@cd deployments && docker compose --profile svc up --build -d  && cd ..
compose-up:
	@cd deployments && docker compose up --build -d && cd ..

compose-down:
	@cd deployments && docker compose down && cd ..

smoke:
	@echo "Waiting for apigateway to be available..."
	@bash -c 'for i in {1..30}; do if curl -sSf http://localhost:8080/health >/dev/null 2>&1; then echo "apigateway ready"; exit 0; fi; sleep 1; done; echo "apigateway did not become ready"; exit 1'

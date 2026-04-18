.PHONY: up down gateway admin-ui dev logs ps health test

COMPOSE := docker compose -f infra/docker-compose.yml

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

gateway:
	cd packages/api-gateway && go run .

admin-ui:
	cd packages/admin-ui && npm run dev

dev: up gateway

health:
	curl -sf http://localhost:3000/health && echo

test:
	cd packages/api-gateway && go test ./...

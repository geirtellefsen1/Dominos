.PHONY: up down gateway dev logs ps health

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

dev: up gateway

health:
	curl -sf http://localhost:3000/health && echo

.PHONY: build run migrate test dev hash-password build-tools schema vet clean docker-build

build:
	go build -o bin/minisms ./cmd/minisms

run:
	go run ./cmd/minisms

migrate:
	migrate -path ./migrations -database $$DATABASE_URL up

test:
	go test ./...

# Loads .env from current directory (bash)
dev:
	@set -a && ( test ! -f .env || . ./.env ) && set +a && go run ./cmd/minisms

hash-password:
	go run ./tools/hashpassword/

build-tools:
	go build -o bin/hashpassword ./tools/hashpassword/

schema:
	@echo "Importing schema to database..."
	@echo "Usage: make schema DB_URL=postgres://minisms:password@localhost/minisms"
	psql "$(DB_URL)" -f deploy/minisms_db.sql

vet:
	go vet ./...

clean:
	rm -rf bin/

docker-build:
	docker build -t minisms:$(shell git describe --tags --always) .
	docker tag minisms:$(shell git describe --tags --always) minisms:latest

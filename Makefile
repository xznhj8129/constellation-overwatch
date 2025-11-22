.PHONY: build run docker-build docker-run clean generate-certs templ-generate templ-watch dev

BINARY_NAME=overwatch
DOCKER_IMAGE=constellation-overwatch:latest

build:
	@echo "Building $(BINARY_NAME)..."
	go build -o bin/$(BINARY_NAME) ./cmd/microlith

run: build
	@echo "Running $(BINARY_NAME)..."
	./bin/$(BINARY_NAME)

docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

docker-stop:
	@echo "Stopping services..."
	docker-compose down

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -rf data/
	rm -rf logs/

generate-certs:
	@echo "Generating self-signed certificates for development..."
	mkdir -p certs
	openssl req -x509 -newkey rsa:4096 -keyout certs/server.key -out certs/server.crt -days 365 -nodes -subj "/CN=localhost"
	chmod 644 certs/server.crt
	chmod 600 certs/server.key
	@echo "Certificates generated in ./certs/"

templ-generate:
	@echo "Generating templ files..."
	go run github.com/a-h/templ/cmd/templ@latest generate

templ-watch:
	@echo "Starting templ file watcher..."
	go run github.com/a-h/templ/cmd/templ@latest generate --watch --proxy="http://localhost:8080" --open-browser=false

dev: templ-generate
	@echo "Starting development mode with templ generation and server..."
	@trap 'kill %1; kill %2' INT; \
	make templ-watch & \
	sleep 2 && make run &

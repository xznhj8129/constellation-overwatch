.PHONY: build run docker-build docker-run clean generate-certs

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

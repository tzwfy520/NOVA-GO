# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
BINARY_NAME=sshcollectorpro
BINARY_UNIX=$(BINARY_NAME)_unix

# Docker parameters
DOCKER_IMAGE=sshcollectorpro
DOCKER_TAG=latest

.PHONY: all build clean test coverage deps tidy lint run docker-build docker-run docker-stop help

all: test build

build:
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/server

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BINARY_UNIX) -v ./cmd/server

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

test:
	$(GOTEST) -v ./...

coverage:
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

deps:
	$(GOGET) -d -v ./...

tidy:
	$(GOMOD) tidy

lint:
	golangci-lint run

run:
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/server
	./$(BINARY_NAME)

docker-build:
	docker build -f deploy/Dockerfile -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run:
	docker run -d --name $(DOCKER_IMAGE) -p 18000:18000 $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-stop:
	docker stop $(DOCKER_IMAGE)
	docker rm $(DOCKER_IMAGE)

help:
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  build-linux  - Build the binary for Linux"
	@echo "  clean        - Clean build artifacts"
	@echo "  test         - Run tests"
	@echo "  coverage     - Run tests with coverage"
	@echo "  deps         - Download dependencies"
	@echo "  tidy         - Tidy go modules"
	@echo "  lint         - Run linter"
	@echo "  run          - Build and run the application"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run Docker container"
	@echo "  docker-stop  - Stop and remove Docker container"
	@echo "  help         - Show this help message"
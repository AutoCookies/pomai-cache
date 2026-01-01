.PHONY: all build test fmt lint docker-build docker-run compose-up compose-down

BINARY := pomai-cache
IMAGE := pomai-cache:local
PORT ?= 8080

all: build

build:
	go build -v -o $(BINARY) ./cmd/server

test:
	go test ./... -v

fmt:
	go fmt ./...

lint:
	@# optional: requires golangci-lint in PATH
	@golangci-lint run || true

docker-build:
	docker build -t $(IMAGE) .

docker-run:
	docker run --rm -p $(PORT):8080 -v $(PWD)/data:/data -e PORT=$(PORT) $(IMAGE)

compose-up:
	docker-compose up --build -d

compose-down:
	docker-compose down
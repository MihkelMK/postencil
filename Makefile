.PHONY: build run fmt lint test docker

build:
	@mkdir -p bin
	go build -o bin/postencil ./cmd/postencil

run:
	go run ./cmd/postencil

fmt:
	golangci-lint fmt ./...

lint:
	golangci-lint run ./...

test:
	go test ./...

test-verbose:
	go test -v ./...

docker:
	docker build -t postencil .

.PHONY: build run fmt lint test docker

build:
	go build -o bin/postencil ./cmd/postencil

run:
	go run ./cmd/postencil

fmt:
	goimports -w .

lint:
	golangci-lint run ./...

test:
	go test ./...

test-verbose:
	go test -v ./...

docker:
	docker build -t postencil .

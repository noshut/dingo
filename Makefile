.PHONY: build test lint run docker

build:
	go build -o bin/dingo ./cmd/dingo

test:
	go test -race ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

run:
	go run ./cmd/dingo -config config.yaml -once

docker:
	docker build -t dingo:latest .

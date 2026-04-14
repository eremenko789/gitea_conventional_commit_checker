APP := webhook-service
BIN := bin/$(APP)

.PHONY: all build test lint cover docker clean

all: build

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/webhook-service

test:
	go test ./...

lint:
	go vet ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

docker:
	docker build -t gitea-conventional-commit-checker:local .

clean:
	rm -rf bin coverage.out

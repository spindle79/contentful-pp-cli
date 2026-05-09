.PHONY: build test lint install clean

build:
	go build -o bin/contentful-pp-cli ./cmd/contentful-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/contentful-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/contentful-pp-mcp ./cmd/contentful-pp-mcp

install-mcp:
	go install ./cmd/contentful-pp-mcp

build-all: build build-mcp

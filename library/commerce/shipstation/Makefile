.PHONY: build test lint install clean

build:
	go build -o bin/shipstation-pp-cli ./cmd/shipstation-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/shipstation-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/shipstation-pp-mcp ./cmd/shipstation-pp-mcp

install-mcp:
	go install ./cmd/shipstation-pp-mcp

build-all: build build-mcp

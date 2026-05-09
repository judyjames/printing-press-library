.PHONY: build test lint install clean

build:
	go build -o bin/table-reservation-goat-pp-cli ./cmd/table-reservation-goat-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/table-reservation-goat-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/table-reservation-goat-pp-mcp ./cmd/table-reservation-goat-pp-mcp

install-mcp:
	go install ./cmd/table-reservation-goat-pp-mcp

build-all: build build-mcp

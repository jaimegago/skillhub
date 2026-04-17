.PHONY: build test lint run clean

BIN := bin/skillhub

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/skillhub

test:
	go test ./...

lint:
	golangci-lint run ./...

run: build
	$(BIN) mcp

clean:
	rm -f $(BIN)

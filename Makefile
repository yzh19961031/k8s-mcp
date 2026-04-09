.PHONY: build test lint clean

BINARY=k8s-mcp
GO=go

build:
	$(GO) build -o $(BINARY) ./main.go

test:
	$(GO) test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)

.DEFAULT_GOAL := build

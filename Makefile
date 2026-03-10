.PHONY: build test lint fmt vet coverage tidy clean

# Default target
all: lint test

# Build all packages
build:
	go build ./...

# Run tests
test:
	go test -race -count=1 ./...

# Run tests with coverage
coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

# Run go vet
vet:
	go vet ./...

# Tidy module dependencies
tidy:
	go mod tidy
	go mod verify

# Clean build artifacts
clean:
	rm -f coverage.out coverage.html

# Install development tools (optional)
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

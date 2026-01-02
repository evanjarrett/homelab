.PHONY: build install clean tidy test

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
BINARY := talos-upgrade

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/talos-upgrade

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-amd64 ./cmd/talos-upgrade
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-arm64 ./cmd/talos-upgrade
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-amd64 ./cmd/talos-upgrade
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-arm64 ./cmd/talos-upgrade

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/talos-upgrade

# Clean build artifacts
clean:
	rm -rf bin/

# Tidy dependencies
tidy:
	go mod tidy

# Run tests
test:
	go test -v ./...

# Run with dry-run
dry-run: build
	./bin/$(BINARY) --dry-run status

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build the binary to bin/"
	@echo "  build-all  - Build for multiple platforms"
	@echo "  install    - Install to GOPATH/bin"
	@echo "  clean      - Remove build artifacts"
	@echo "  tidy       - Tidy go.mod dependencies"
	@echo "  test       - Run tests"
	@echo "  dry-run    - Build and run status in dry-run mode"
	@echo "  help       - Show this help"

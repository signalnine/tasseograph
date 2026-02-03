# Tasseograph Makefile

BINARY_NAME := tasseograph
DIST_DIR := dist
COVERAGE_FILE := coverage.out
MAIN_PKG := ./cmd/tasseograph

.PHONY: build build-linux test test-cover clean lint fmt

# Build for current platform
build:
	@mkdir -p $(DIST_DIR)
	go build -o $(DIST_DIR)/$(BINARY_NAME) $(MAIN_PKG)

# Build for linux/amd64 and linux/arm64
build-linux:
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PKG)
	GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PKG)

# Run all tests
test:
	go test ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=$(COVERAGE_FILE) ./...

# Remove build artifacts
clean:
	rm -rf $(DIST_DIR)
	rm -f $(COVERAGE_FILE)

# Run linter (golangci-lint if available, otherwise go vet)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		go vet ./...; \
	fi

# Format code
fmt:
	go fmt ./...

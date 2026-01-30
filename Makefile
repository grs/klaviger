.PHONY: build test lint clean run install-tools

# Build variables
BINARY_NAME=klaviger
BUILD_DIR=bin
MAIN_PATH=cmd/klaviger/main.go

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete"

# Run tests with coverage
test-coverage: test
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linting
lint:
	@echo "Running linters..."
	$(GOFMT) -s -w .
	$(GOVET) ./...
	@echo "Linting complete"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME) --config examples/config.yaml

# Tidy dependencies
tidy:
	$(GOMOD) tidy
	$(GOMOD) verify

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed"

# Run integration tests
integration-test:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./test/integration/...
	@echo "Integration tests complete"

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):latest -f deployments/Dockerfile .
	@echo "Docker build complete"

# Multi-platform Docker build
docker-buildx:
	@echo "Building multi-platform Docker image..."
	docker buildx build --platform linux/amd64,linux/arm64,linux/ppc64le,linux/s390x \
		-t $(BINARY_NAME):latest -f deployments/Dockerfile .
	@echo "Multi-platform Docker build complete"

# Help
help:
	@echo "Available targets:"
	@echo "  build            - Build the binary"
	@echo "  test             - Run unit tests"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  lint             - Run linters"
	@echo "  clean            - Remove build artifacts"
	@echo "  run              - Build and run the application"
	@echo "  tidy             - Tidy and verify dependencies"
	@echo "  install-tools    - Install development tools"
	@echo "  integration-test - Run integration tests"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-buildx    - Build multi-platform Docker image"
	@echo "  help             - Show this help message"

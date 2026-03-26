.PHONY: build test lint clean run install-tools podman-build podman-push deploy-openshift undeploy-openshift

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

# Container build variables
CONTAINER_ENGINE ?= podman
PODMAN_CONNECTION ?= rhel
REGISTRY ?= ghcr.io/pavelanni
IMAGE_NAME ?= klaviger
GIT_SHA := $(shell git rev-parse --short HEAD)
GIT_DIRTY := $(shell git diff --quiet 2>/dev/null || echo '-dirty')
DEV_TAG ?= $(GIT_SHA)$(GIT_DIRTY)
FULL_IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(DEV_TAG)

# Deploy variables
DEPLOY_NAMESPACE ?= spiffe-demo
DEPLOY_DIR = deploy/openshift

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

# Podman build (remote, for x86_64)
DOCKERFILE ?= deployments/Dockerfile.alpine
podman-build:
	@echo "Building $(FULL_IMAGE)..."
	$(CONTAINER_ENGINE) --connection=$(PODMAN_CONNECTION) build \
		-t $(FULL_IMAGE) \
		-f $(DOCKERFILE) .
	@echo "Build complete: $(FULL_IMAGE)"

# Push to GHCR
podman-push:
	@echo "Pushing $(FULL_IMAGE)..."
	$(CONTAINER_ENGINE) --connection=$(PODMAN_CONNECTION) push $(FULL_IMAGE)
	@echo "Push complete"

# Deploy to OpenShift
deploy-openshift:
	@echo "=== Deploying to OpenShift with tag $(DEV_TAG) ==="
	cd $(DEPLOY_DIR)/base && \
		kustomize edit set image $(REGISTRY)/$(IMAGE_NAME):$(DEV_TAG)
	kustomize build $(DEPLOY_DIR)/base | oc apply -n $(DEPLOY_NAMESPACE) -f -
	@echo "Deployed with image tag: $(DEV_TAG)"

# Undeploy from OpenShift
undeploy-openshift:
	kustomize build $(DEPLOY_DIR)/base | oc delete -n $(DEPLOY_NAMESPACE) -f - --ignore-not-found

# Help
help:
	@echo "Available targets:"
	@echo "  build              - Build the binary"
	@echo "  test               - Run unit tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  lint               - Run linters"
	@echo "  clean              - Remove build artifacts"
	@echo "  run                - Build and run the application"
	@echo "  tidy               - Tidy and verify dependencies"
	@echo "  install-tools      - Install development tools"
	@echo "  integration-test   - Run integration tests"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-buildx      - Build multi-platform Docker image"
	@echo "  podman-build       - Build image with Podman (remote x86_64)"
	@echo "  podman-push        - Push image to GHCR"
	@echo "  deploy-openshift   - Deploy to OpenShift (uses git SHA tag)"
	@echo "  undeploy-openshift - Remove deployment from OpenShift"
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Variables:"
	@echo "  DEV_TAG            - Image tag (default: git SHA, e.g., abc1234)"
	@echo "  DEPLOY_NAMESPACE   - Target namespace (default: spiffe-demo)"

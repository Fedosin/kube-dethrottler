GO_CMD=go
GO_BUILD=$(GO_CMD) build
GO_TEST=$(GO_CMD) test
GO_LINT=golangci-lint run
GO_CLEAN=$(GO_CMD) clean

BINARY_NAME=kube-dethrottler
BIN_PATH=./bin
CMD_PATH=./cmd/kube-dethrottler

VERSION ?= $(shell git describe --tags --always --dirty)
IMAGE_NAME ?= fedosin/kube-dethrottler
IMAGE_TAG ?= $(VERSION)

# Docker command, can be docker or podman, etc.
DOCKER_CMD ?= docker

all: build

build: ## Build the Go binary
	@echo "Building $(BINARY_NAME) version $(VERSION)"
	@mkdir -p $(BIN_PATH)
	$(GO_BUILD) -ldflags="-w -s -X main.Version=$(VERSION)" -o $(BIN_PATH)/$(BINARY_NAME) $(CMD_PATH)/main.go

clean: ## Clean up build artifacts
	@echo "Cleaning up build artifacts"
	$(GO_CLEAN)
	rm -f $(BIN_PATH)/$(BINARY_NAME)

test: ## Run unit tests
	@echo "Running unit tests"
	$(GO_TEST) -v ./...

lint: ## Run linters
	@echo "Running linters"
	$(GO_LINT)

lint-fix: ## Run linters and fix issues
	@echo "Running linters and fixing issues"
	$(GO_LINT) --fix

docker-build: build ## Build Docker image
	@echo "Building Docker image"
	$(DOCKER_CMD) build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-push: ## Push Docker image to registry
	@echo "Pushing Docker image to registry"
	$(DOCKER_CMD) push $(IMAGE_NAME):$(IMAGE_TAG)

helm-install: ## Install Helm chart (assuming chart is in ./charts/kube-dethrottler)
	@echo "Make sure your Helm chart exists at ./charts/kube-dethrottler"
	helm install kube-dethrottler ./charts/kube-dethrottler --namespace kube-system --create-namespace

helm-upgrade: ## Upgrade Helm release
	@echo "Upgrading Helm release"
	helm upgrade kube-dethrottler ./charts/kube-dethrottler --namespace kube-system

helm-uninstall: ## Uninstall Helm release
	@echo "Uninstalling Helm release"
	helm uninstall kube-dethrottler --namespace kube-system

e2e: ## Placeholder for end-to-end tests
	@echo "End-to-end tests are not yet implemented. Please configure Kind/Minikube and test scripts."

.PHONY: all build clean test lint docker-build docker-push helm-install helm-upgrade helm-uninstall e2e

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' 
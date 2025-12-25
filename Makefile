# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/lukaszraczylo/kubemirror:latest
IMG_SECONDARY_TAG ?= ""

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: ## Run staticcheck, gosec, and other linters.
	@command -v staticcheck >/dev/null 2>&1 || { echo "Installing staticcheck..."; go install honnef.co/go/tools/cmd/staticcheck@latest; }
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec..."; go install github.com/securego/gosec/v2/cmd/gosec@latest; }
	@command -v deadcode >/dev/null 2>&1 || { echo "Installing deadcode..."; go install golang.org/x/tools/cmd/deadcode@latest; }
	staticcheck ./...
	gosec -exclude=G115 ./...
	deadcode ./...

.PHONY: test
test: fmt vet ## Run tests.
	go test ./... -coverprofile cover.out

.PHONY: test-race
test-race: fmt vet ## Run tests with race detector.
	go test -race ./...

.PHONY: test-verbose
test-verbose: fmt vet ## Run tests with verbose output.
	go test -v -race ./...

.PHONY: bench
bench: ## Run benchmarks.
	go test -race -bench=. -benchmem ./...

.PHONY: cover
cover: test ## Run tests and open coverage in browser.
	go tool cover -html=cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build controller binary.
	go build -o kubemirror ./cmd/kubemirror

.PHONY: run
run: fmt vet ## Run controller from your host (against current kubeconfig).
	go run ./cmd/kubemirror --dry-run=true

.PHONY: clean
clean: ## Clean build artifacts.
	rm -f kubemirror cover.out
	rm -rf dist/

.PHONY: docker-build
docker-build: test ## Build docker image.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image.
	docker push ${IMG}

# PLATFORMS defines the target platforms for the manager image
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for cross-platform support
	- docker buildx create --name kubemirror-builder
	docker buildx use kubemirror-builder
	if [ -z "$(IMG_SECONDARY_TAG)" ]; then \
		docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} .; \
	else \
		docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} --tag ${IMG_SECONDARY_TAG} .; \
	fi
	- docker buildx rm kubemirror-builder

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	kubectl apply -k deploy/

.PHONY: undeploy
undeploy: ## Remove controller from the K8s cluster.
	kubectl delete -k deploy/ --ignore-not-found=$(ignore-not-found)

.PHONY: logs
logs: ## Show controller logs.
	kubectl -n kubemirror-system logs -l app.kubernetes.io/name=kubemirror -f

.PHONY: status
status: ## Show controller status.
	kubectl -n kubemirror-system get pods -l app.kubernetes.io/name=kubemirror
	kubectl -n kubemirror-system get deployments
	kubectl -n kubemirror-system get services

##@ Code Quality

.PHONY: tidy
tidy: ## Run go mod tidy.
	go mod tidy

.PHONY: verify
verify: fmt vet lint test-race ## Run all verification steps (format, vet, lint, test with race).

.PHONY: ci
ci: verify bench ## Run full CI pipeline locally.

##@ Release

.PHONY: release-dry
release-dry: ## Run GoReleaser in dry-run mode.
	@command -v goreleaser >/dev/null 2>&1 || { echo "Installing goreleaser..."; go install github.com/goreleaser/goreleaser@latest; }
	goreleaser release --snapshot --clean

.PHONY: release
release: ## Run GoReleaser (requires tag).
	@command -v goreleaser >/dev/null 2>&1 || { echo "Installing goreleaser..."; go install github.com/goreleaser/goreleaser@latest; }
	goreleaser release --clean

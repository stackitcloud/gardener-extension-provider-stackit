ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_DIR                := $(shell go list -mod=mod -m -f "{{.Dir}}" github.com/gardener/gardener)
GARDENER_HACK_DIR           := $(GARDENER_DIR)/hack

EXTENSION_PREFIX            := gardener-extension
NAME                        := provider-stackit
ADMISSION                   := admission-stackit
REPO                        := ghcr.io/stackitcloud
IS_DEV                      ?= true
ifeq ($(IS_DEV),true)
REPO_POSTFIX                := -dev
endif
REPO_ROOT                   := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                    := $(REPO_ROOT)/hack
VERSION                     := $(shell git describe --tag --always --dirty)
TAG							:= $(VERSION)
LEADER_ELECTION             := false

REGION             := eu01
FLOATING_POOL_NAME := floating-net

INFRA_TEST_FLAGS   := --region='$(REGION)'

SHELL=/bin/bash -e -o pipefail

#########################################
# Tools                                 #
#########################################

TOOLS_DIR := $(HACK_DIR)/tools
include $(GARDENER_HACK_DIR)/tools.mk
include $(HACK_DIR)/tools.mk

.PHONY: run
run: ## Starts the application locally
	@LEADER_ELECTION_NAMESPACE=garden GO111MODULE=on go run \
		./cmd/$(EXTENSION_PREFIX)-$(NAME) \
		--kubeconfig=${KUBECONFIG} \
		--leader-election=$(LEADER_ELECTION)

.PHONY: debug
debug: ## Starts the application locally with a delve as a debugger
	@LEADER_ELECTION_NAMESPACE=garden GO111MODULE=on dlv debug\
		./cmd/$(EXTENSION_PREFIX)-$(NAME) -- \
		--kubeconfig=${KUBECONFIG} \
		--leader-election=$(LEADER_ELECTION)

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

PUSH ?= false
OUTPUT_IMAGES_PATH = "images.txt"

images: export KO_DOCKER_REPO = $(REPO)

.PHONY: images
images: $(KO) ## Builds a container image with the app using ko. Use PUSH=True to also push the image to a registry
	KO_DOCKER_REPO=$(REPO)/$(EXTENSION_PREFIX)-$(NAME)$(REPO_POSTFIX) \
	$(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-provider-stackit" \
	--sbom none -t $(TAG) --bare \
	--platform linux/amd64,linux/arm64 --push=$(PUSH) \
	./cmd/$(EXTENSION_PREFIX)-$(NAME) \
	| tee $(OUTPUT_IMAGES_PATH)
	@jq -n --arg key "$(EXTENSION_PREFIX)-$(NAME)" --arg value "$$(cat $(OUTPUT_IMAGES_PATH))" '{images: {($$key): $$value}}' > images.json

.PHONY: admission-images
admission-images: $(KO) ## Builds a container image with the app using ko. Use PUSH=True to also push the image to a registry
	KO_DOCKER_REPO=$(REPO)/$(EXTENSION_PREFIX)-$(ADMISSION)$(REPO_POSTFIX) \
	$(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-provider-stackit" \
	--sbom none -t $(TAG) --bare \
	--platform linux/amd64,linux/arm64 --push=$(PUSH) \
	./cmd/$(EXTENSION_PREFIX)-$(ADMISSION) \
	| tee admission-images.txt
	@jq --arg image "$$(cat admission-images.txt)" '.images += {"$(EXTENSION_PREFIX)-$(ADMISSION)":$$image}' images.json > images.json.tmp
	@mv -v images.json.tmp images.json

.PHONY: artifacts-only
artifacts-only: $(YQ) $(HELM) ## Builds helm charts (`charts/`)
	PUSH=$(PUSH) hack/push-artifacts.sh images.json

.PHONY: artifacts
artifacts: images admission-images artifacts-only ## Builds all artifacts.

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: tidy
tidy:
	@GO111MODULE=on go mod tidy

# run `make init` to perform an initial go mod cache sync which is required for other make targets
init: tidy ## Run `make init` to perform an initial go mod cache sync which is required for other make targets
# needed so that check-generate.sh can call make revendor
revendor: tidy

.PHONY: clean
clean: ## Cleans the ./cmd and ./pkg package
	@bash $(GARDENER_HACK_DIR)/clean.sh ./cmd/... ./pkg/...

.PHONY: check-generate
check-generate: ## Check if check target has been run
	@bash $(GARDENER_HACK_DIR)/check-generate.sh $(REPO_ROOT)

.PHONY: check
check: $(GOIMPORTS) $(GOLANGCI_LINT) $(HELM) ## Runs golangci-lint, gofmt/goimports and checks the chart for validity
	@bash $(GARDENER_HACK_DIR)/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/... ./test...
	@bash $(GARDENER_HACK_DIR)/check-charts.sh ./charts

# generate mock types for the following services from the SDK (space-separated list)
SDK_MOCK_SERVICES := iaas loadbalancer dns

.PHONY: generate-mocks
generate-mocks: $(MOCKGEN)
	@echo "Running $(MOCKGEN)"
	@go mod download
	@for service in $(SDK_MOCK_SERVICES); do \
		INTERFACES=`go doc -all github.com/stackitcloud/stackit-sdk-go/services/$$service | grep '^type Api.* interface' | sed -n 's/^type \(.*\) interface.*/\1/p' | paste -sd,`,DefaultApi; \
		$(MOCKGEN) -destination ./pkg/stackit/client/mock/$$service/$$service.go -package $$service github.com/stackitcloud/stackit-sdk-go/services/$$service $$INTERFACES; \
	done

.PHONY: generate
generate: $(CONTROLLER_GEN) $(GEN_CRD_API_REFERENCE_DOCS) $(HELM) $(MOCKGEN) $(YQ) $(YAML2JSON) $(GOIMPORTS) generate-mocks ## Generates the controller-registration, other code-gen, the imagename constants as well as executes go:generate directives
	@cd imagevector && bash $(GARDENER_HACK_DIR)/generate-imagename-constants.sh
	@REPO_ROOT=$(REPO_ROOT) VGOPATH=$(VGOPATH) GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) bash $(GARDENER_HACK_DIR)/generate-sequential.sh ./charts/... ./cmd/... ./pkg/...

.PHONY: format
format: $(GOIMPORTS) $(GOIMPORTSREVISER) ## Formats all files in ./cmd, ./pkg and ./test
	@bash $(GARDENER_HACK_DIR)/format.sh ./cmd ./pkg ./test

check-format: format
	@if !(git diff --quiet HEAD); then \
		echo "Unformatted files detected please run 'make format'"; exit 1; \
	fi

UPSTREAM_CRDS_DIR=$(REPO_ROOT)/test/integration/testdata/upstream-crds
upstream-crds: cleanup-crds gardener-crds ## Pulls up-to-date versions of the upstream CRDs

.PHONY: cleanup-crds
cleanup-crds:
	@rm -f $(UPSTREAM_CRDS_DIR)/*

.PHONY: gardener-crds
gardener-crds:
	@cp $(GARDENER_DIR)/example/seed-crds/10-crd-extensions.gardener.cloud_clusters.yaml $(UPSTREAM_CRDS_DIR)
	@cp $(GARDENER_DIR)/example/seed-crds/10-crd-extensions.gardener.cloud_infrastructures.yaml $(UPSTREAM_CRDS_DIR)

.PHONY: test
test: $(REPORT_COLLECTOR) $(SETUP_ENVTEST) ## Runs the unit-test suite
	@./hack/test.sh ./cmd/... ./pkg/...

.PHONY: test-cov
test-cov:
	@bash $(GARDENER_HACK_DIR)/test-cover.sh ./cmd/... ./pkg/...

.PHONY: verify
verify: check check-format test ## Run check, format and test

.PHONY: verify-extended
verify-extended: check-generate check check-format test ## Run check-generate, check, format and test

.PHONY: test-integration-infra
test-integration-infra: $(REPORT_COLLECTOR) $(SETUP_ENVTEST) $(GINKGO) ## Run infrastructure integration tests
	@GINKGO=$(GINKGO) ./hack/test-integration.sh \
		-v --show-node-events \
		--procs 2 --timeout 6m \
		--grace-period 2m \
		./test/integration/infrastructure/stackit \
		-- \
		$(INFRA_TEST_FLAGS)

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

#####################################################################
# Rules for local environment                                       #
#####################################################################

# speed-up skaffold deployments by building all images concurrently
extension-%: export SKAFFOLD_BUILD_CONCURRENCY = 0
extension-%: export SKAFFOLD_DEFAULT_REPO = ghcr.io
extension-%: export SKAFFOLD_PUSH = true
# use static label for skaffold to prevent rolling all gardener components on every `skaffold` invocation
extension-%: export SKAFFOLD_LABEL = skaffold.dev/run-id=gardener-extension-provider-stackit

extension-up: $(SKAFFOLD)
	GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) $(SKAFFOLD) run
extension-dev: $(SKAFFOLD)
	$(SKAFFOLD) dev --cleanup=false --trigger=manual
extension-down: $(SKAFFOLD)
	$(SKAFFOLD) delete

mirrord-debug: ## Debug the provider using mirrord
	./hack/mirrord.sh debug

mirrord-run: ## Run your current version in-cluster
	./hack/mirrord.sh run

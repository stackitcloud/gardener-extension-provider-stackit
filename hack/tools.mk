# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.19.1
# renovate: datasource=github-releases depName=uber-go/mock
MOCKGEN_VERSION = v0.6.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION = v2.15.0
# renovate: datasource=github-releases depName=fluxcd/flux2
FLUX_VERSION ?= 2.5.1

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

FLUX := $(TOOLS_BIN_DIR)/flux
$(FLUX): $(call tool_version_file,$(FLUX),$(FLUX_VERSION))
	curl -s https://fluxcd.io/install.sh | FLUX_VERSION=$(FLUX_VERSION) bash -s $(TOOLS_BIN_DIR)
	touch $(FLUX) && chmod +x $(FLUX)

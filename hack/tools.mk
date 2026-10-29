# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.19.1
# renovate: datasource=github-releases depName=uber-go/mock
MOCKGEN_VERSION = v0.6.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION = v2.15.0
# renovate: datasource=github-tags depName=fluxcd/flux2
FLUX_VERSION ?= v2.10.0

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

FLUX := $(TOOLS_BIN_DIR)/flux
$(FLUX): $(call tool_version_file,$(FLUX),$(FLUX_VERSION))
	curl -sSLo - https://github.com/fluxcd/flux2/releases/download/$(FLUX_VERSION)/flux_$(FLUX_VERSION:v%=%)_$(shell go env GOOS)_$(shell go env GOARCH).tar.gz | tar -xzmf - -C $(TOOLS_BIN_DIR) flux
	chmod +x $(FLUX)

# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.18.1
# renovate: datasource=github-releases depName=uber-go/mock
MOCKGEN_VERSION = v0.6.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION = v2.10.1

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

LINT_VERSION := v2.12.2
BIN_DIR := $(CURDIR)/bin
LINT := $(BIN_DIR)/golangci-lint

.PHONY: tools lint lint-fix fmt

tools: $(LINT)

$(LINT):
	@mkdir -p $(BIN_DIR)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN_DIR) $(LINT_VERSION)

lint: tools
	$(LINT) run ./...

lint-fix: tools
	$(LINT) run --fix ./...

fmt: tools
	$(LINT) fmt ./...

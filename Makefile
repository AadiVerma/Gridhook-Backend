LINT_VERSION := v2.12.2
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
LINT_PKG := golangci-lint-$(LINT_VERSION:v%=%)-$(GOOS)-$(GOARCH)
LINT_BASE_URL := https://github.com/golangci/golangci-lint/releases/download/$(LINT_VERSION)
BIN_DIR := $(CURDIR)/bin
LINT := $(BIN_DIR)/golangci-lint

.PHONY: tools lint lint-fix fmt

tools: $(LINT)

# Downloads the release tarball straight from GitHub and verifies it against
# the published checksums.txt before extracting — golangci-lint's own
# install.sh has been observed serving a mismatched asset (it hashed the
# release's .sbom.json instead of the tarball), so we don't shell out to it.
$(LINT):
	@mkdir -p $(BIN_DIR)
	@tmp=$$(mktemp -d); \
	curl -sSfL -o $$tmp/$(LINT_PKG).tar.gz $(LINT_BASE_URL)/$(LINT_PKG).tar.gz; \
	curl -sSfL -o $$tmp/checksums.txt $(LINT_BASE_URL)/golangci-lint-$(LINT_VERSION:v%=%)-checksums.txt; \
	cd $$tmp && grep "$(LINT_PKG).tar.gz$$" checksums.txt | shasum -a 256 -c - && \
	tar -xzf $(LINT_PKG).tar.gz -C $$tmp --strip-components=1 $(LINT_PKG)/golangci-lint && \
	mv $$tmp/golangci-lint $(LINT) && \
	rm -rf $$tmp

lint: tools
	$(LINT) run ./...

lint-fix: tools
	$(LINT) run --fix ./...

fmt: tools
	$(LINT) fmt ./...

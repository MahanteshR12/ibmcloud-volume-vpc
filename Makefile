
GOPACKAGES=$(shell go list ./... | grep -v /vendor/ | grep -v /samples)
GOFILES=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
ARCH = $(shell uname -m)
LINT_VERSION="1.62.2"

GOPATH := $(shell go env GOPATH)
# Use system golangci-lint if available, otherwise use GOPATH version
LINT_BIN=$(shell which golangci-lint 2>/dev/null || echo $(GOPATH)/bin/golangci-lint)

.PHONY: all
all: deps fmt vet test

.PHONY: deps
deps:
	echo "Installing dependencies ..."
	go mod download

	@if ! command -v gotestcover >/dev/null; then \
		echo "Installing gotestcover ..."; \
		go install github.com/pierrre/gotestcover@latest; \
	fi

	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint $(LINT_VERSION) ..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@v$(LINT_VERSION); \
	else \
		echo "golangci-lint already installed at $$(which golangci-lint)"; \
	fi

.PHONY: fmt
fmt:
	$(LINT_BIN) run --enable=gofmt --timeout=10m

.PHONY: dofmt
dofmt:
	$(LINT_BIN) run --enable=gofmt --fix --timeout=10m

.PHONY: lint
lint:
	$(LINT_BIN) run

.PHONY: gofmt
gofmt:
	gofmt -l -w ${GOFILES}

.PHONY: build
build:
	go build -gcflags '-N -l' -o libSample samples/main.go samples/attach_detach.go samples/volume_operations.go

.PHONY: test
test:
	@echo "Running per-package tests with merged coverage..."
	@rm -f cover.out
	@for pkg in ${GOPACKAGES}; do \
		echo "=== Testing $$pkg ==="; \
		if echo "$$pkg" | grep -q "block/provider"; then \
			echo "Running $$pkg without coverpkg to avoid hang"; \
			go test -v $$pkg -coverprofile=tmp.out -timeout 90m || exit 1; \
		else \
			go test -v $$pkg -coverpkg=./... -coverprofile=tmp.out -timeout 90m || exit 1; \
		fi; \
		if [ -f tmp.out ]; then \
			tail -n +2 tmp.out >> cover.out; \
			rm tmp.out; \
		fi; \
	done

.PHONY: coverage
coverage:
	go tool cover -html=cover.out -o=cover.html
	./scripts/calculateCoverage.sh

.PHONY: vet
vet:
	go vet ${GOPACKAGES}

clean:
	rm -rf libSample

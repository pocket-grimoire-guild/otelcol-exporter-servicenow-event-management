SHELL := /usr/bin/env bash

OTEL_COLLECTOR_VERSION ?= v0.153.0
TOOLS_BIN ?= $(CURDIR)/.tools/bin
TOOLS_PATH := $(TOOLS_BIN):$(PATH)

.PHONY: install-tools verify test race lint docs-check generate build-collector clean

install-tools:
	@GOBIN="$(TOOLS_BIN)" OTEL_COLLECTOR_VERSION="$(OTEL_COLLECTOR_VERSION)" ./scripts/install-collector-tools.sh

verify:
	@PATH="$(TOOLS_PATH)" OTEL_COLLECTOR_VERSION="$(OTEL_COLLECTOR_VERSION)" ./scripts/verify.sh

test:
	@if [[ -f go.mod ]]; then \
		if command -v gotestsum >/dev/null 2>&1; then gotestsum -- ./...; else go test ./...; fi; \
	else \
		echo "skip: go.mod not found"; \
	fi

race:
	@if [[ -f go.mod ]]; then \
		go test -race ./...; \
	else \
		echo "skip: go.mod not found"; \
	fi

lint:
	@if [[ -f go.mod ]]; then \
		if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "skip: golangci-lint not installed"; fi; \
	else \
		echo "skip: go.mod not found"; \
	fi

docs-check:
	./scripts/check-docs.sh

generate:
	@if find . -name metadata.yaml -print -quit | grep -q .; then \
		PATH="$(TOOLS_PATH)" ./scripts/check-collector-tool-version.sh mdatagen go.opentelemetry.io/collector/cmd/mdatagen "$(OTEL_COLLECTOR_VERSION)" >/dev/null; \
		PATH="$(TOOLS_PATH)" go generate ./...; \
	else \
		echo "skip: no metadata.yaml found"; \
	fi

build-collector:
	@if PATH="$(TOOLS_PATH)" command -v builder >/dev/null 2>&1; then \
		PATH="$(TOOLS_PATH)" ./scripts/check-collector-tool-version.sh builder go.opentelemetry.io/collector/cmd/builder "$(OTEL_COLLECTOR_VERSION)" >/dev/null; \
		PATH="$(TOOLS_PATH)" builder --config builder-config.yaml; \
	elif PATH="$(TOOLS_PATH)" command -v ocb >/dev/null 2>&1; then \
		PATH="$(TOOLS_PATH)" ./scripts/check-collector-tool-version.sh ocb go.opentelemetry.io/collector/cmd/builder "$(OTEL_COLLECTOR_VERSION)" >/dev/null; \
		PATH="$(TOOLS_PATH)" ocb --config builder-config.yaml; \
	else \
		echo "collector builder not installed; run make install-tools" >&2; \
		exit 1; \
	fi

clean:
	rm -rf otelcol-servicenow-event-management-dev dist build bin coverage.out

SHELL := bash

ARTIFACT_NAME := external-dns-efficientip-webhook

TESTPARALLELISM := 4

WORKING_DIR := $(shell pwd)

.PHONY: clean
clean::
	rm -rf $(WORKING_DIR)/bin

.PHONY: build
build::
	go build -o $(WORKING_DIR)/bin/${ARTIFACT_NAME} ./cmd/webhook
	chmod +x $(WORKING_DIR)/bin/${ARTIFACT_NAME}

.PHONY: test
test::
	go test -v -tags=all -parallel ${TESTPARALLELISM} -timeout 2h -covermode atomic -coverprofile=covprofile ./...

.PHONY: lint
lint: ## Run golangci-lint against code.
	mkdir -p build/reports
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run --timeout 2m
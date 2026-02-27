# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=schemafixer
BINARY_WINDOWS=$(BINARY_NAME).exe
BINARY_LINUX=$(BINARY_NAME)
BUILDDIR=build
VERSION=dev-latest

.PHONY: all build build-windows build-linux clean test test-ci check pre-commit

all: build

# Build targets
build: build-windows build-linux

build-windows:
	env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o $(BUILDDIR)/$(BINARY_WINDOWS) -ldflags "-X main.version=$(VERSION) -w -s -extldflags=-static" ./cmd/schemafixer

build-linux:
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(BUILDDIR)/$(BINARY_LINUX) -ldflags "-X main.version=$(VERSION) -w -s -extldflags=-static" ./cmd/schemafixer

# Build with UPX compression (requires UPX installed)
build-compressed: build-windows-compressed build-linux-compressed

build-windows-compressed:
	env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o $(BUILDDIR)/$(BINARY_WINDOWS) -ldflags "-X main.version=$(VERSION) -w -s -extldflags=-static" ./cmd/schemafixer
	upx --best --lzma $(BUILDDIR)/$(BINARY_WINDOWS)

build-linux-compressed:
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(BUILDDIR)/$(BINARY_LINUX) -ldflags "-X main.version=$(VERSION) -w -s -extldflags=-static" ./cmd/schemafixer
	upx --best --lzma $(BUILDDIR)/$(BINARY_LINUX)

# Test targets
test:
	$(GOTEST) -v ./...

test-ci:
	$(GOTEST) -v -race -coverprofile=coverage.txt -covermode=atomic ./...

test-coverage:
	$(GOTEST) -v -coverprofile=coverage.txt -covermode=atomic ./...

# Quick check (verify + vet + test)
check:
	$(GOCMD) mod verify
	$(GOCMD) vet ./...
	$(GOTEST) -v ./...

# Complete pre-commit check (Windows-compatible - no race detection)
pre-commit:
	$(GOCMD) mod verify
	$(GOBUILD) -v ./cmd/schemafixer
	$(GOCMD) vet ./...
	$(GOTEST) -v -coverprofile=coverage.txt -covermode=atomic ./...

# CI check (Linux - with race detection)
ci:
	$(GOCMD) mod verify
	$(GOBUILD) -v ./cmd/schemafixer
	$(GOCMD) vet ./...
	$(GOTEST) -v -race -coverprofile=coverage.txt -covermode=atomic ./...

clean:
	if exist schemafixer.exe del schemafixer.exe
	if exist schemafixer del schemafixer
	if exist annotations.json del annotations.json
	if exist annotations.log del annotations.log
	if exist coverage.txt del coverage.txt

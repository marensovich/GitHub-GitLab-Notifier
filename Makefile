.PHONY: help version deps run run-ru run-en run-args build build-all release clean test cover fmt vet lint install docker docker-run

BINARY_NAME=GitHub-GitLab-Notifier
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.1")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
GOBUILD=CGO_ENABLED=0 go build -trimpath $(LDFLAGS)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

version: ## Print version information
	@echo "Version: $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"

deps: ## Tidy Go modules
	go mod tidy

run: ## Run the bot locally
	go run .

run-ru: ## Run with Russian as default language
	go run . --lang=ru

run-en: ## Run with English as default language
	go run . --lang=en

run-args: ## Run with custom arguments: make run-args ARGS="--config prod.env"
	go run . $(ARGS)

build: ## Build a static binary for the current platform
	$(GOBUILD) -o $(BINARY_NAME) .

build-all: ## Cross-compile for all supported platforms into build/
	@echo "🔨 Building for all platforms..."
	@mkdir -p build
	GOOS=linux   GOARCH=amd64         $(GOBUILD) -o build/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux   GOARCH=arm64         $(GOBUILD) -o build/$(BINARY_NAME)-linux-arm64 .
	GOOS=linux   GOARCH=arm   GOARM=7 $(GOBUILD) -o build/$(BINARY_NAME)-linux-armv7 .
	GOOS=darwin  GOARCH=amd64         $(GOBUILD) -o build/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64         $(GOBUILD) -o build/$(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64         $(GOBUILD) -o build/$(BINARY_NAME)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64         $(GOBUILD) -o build/$(BINARY_NAME)-windows-arm64.exe .
	GOOS=freebsd GOARCH=amd64         $(GOBUILD) -o build/$(BINARY_NAME)-freebsd-amd64 .
	GOOS=freebsd GOARCH=arm64         $(GOBUILD) -o build/$(BINARY_NAME)-freebsd-arm64 .
	@echo "✅ Done! Files in build/:"
	@ls -lh build/

release: build-all ## Build all platforms and pack them into archives
	@echo "📦 Creating release archives..."
	@cd build && for file in $(BINARY_NAME)-*; do \
		case "$$file" in \
			*.tar.gz|*.zip) ;; \
			*.exe) zip -j "$${file%.exe}.zip" "$$file" && rm "$$file" ;; \
			*) tar -czf "$$file.tar.gz" "$$file" && rm "$$file" ;; \
		esac; \
	done
	@cd build && sha256sum * > checksums.txt
	@echo "✅ Release archives:"
	@ls -lh build/

clean: ## Remove build artifacts
	rm -rf build
	rm -f $(BINARY_NAME) coverage.out

test: ## Run tests
	go test -count=1 ./...

cover: ## Run tests with a coverage report
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

fmt: ## Format Go code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

lint: vet ## Check formatting, vet and shell scripts
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt':"; gofmt -l .; exit 1)
	@command -v shellcheck >/dev/null && shellcheck -x scripts/*.sh || echo "shellcheck not installed, skipping"

install: ## Install as a systemd service on this Linux machine (builds from source)
	sudo ./scripts/install.sh install --from-source

docker: ## Build the Docker image
	docker build --build-arg VERSION=$(VERSION) -t github-gitlab-notifier:$(VERSION) -t github-gitlab-notifier:latest .

docker-run: ## Run with docker compose
	docker compose up -d --build

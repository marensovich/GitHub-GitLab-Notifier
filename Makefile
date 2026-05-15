.PHONY: run build build-all clean deps test

BINARY_NAME=GitHub-GitLab-Notifier
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.1")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

version:
	@echo "Version: $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"

deps:
	go mod tidy

run:
	go run main.go

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) main.go

build-all:
	@echo "🔨 Building for all platforms..."
	@mkdir -p build

	# Linux (amd64)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-linux-amd64 main.go

	# Linux (arm64 — Raspberry Pi, AWS Graviton)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-linux-arm64 main.go

	# macOS (Intel)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-darwin-amd64 main.go

	# macOS (Apple Silicon M1/M2)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-darwin-arm64 main.go

	# Windows (64-bit)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-windows-amd64.exe main.go

	# FreeBSD
	GOOS=freebsd GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-freebsd-amd64 main.go

	@echo "✅ Done! Files in build/:"
	@ls -lh build/

release: build-all
	@echo "📦 Creating release archives..."
	@cd build && \
		for file in *; do \
			if [ "$$file" != "*.tar.gz" ] && [ "$$file" != "*.zip" ]; then \
				if echo "$$file" | grep -q '.exe$$'; then \
					zip "$$file.zip" "$$file" && rm "$$file"; \
				else \
					tar -czf "$$file.tar.gz" "$$file" && rm "$$file"; \
				fi; \
			fi; \
		done
	@echo "✅ Release archives:"
	@ls -lh build/

clean:
	rm -rf build
	rm -f $(BINARY_NAME)

test:
	go test ./...
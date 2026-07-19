VERSION ?= $(shell tr -d '[:space:]' < release/VERSION)
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build release clean run deps

# Local build (current OS)
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/yuri_scraper-v$(VERSION) ./cmd/yuri_scraper/

# Release builds — cross-compile for all targets
release: clean
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yuri-scraper-v$(VERSION)-linux-amd64 ./cmd/yuri_scraper/
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yuri-scraper-v$(VERSION)-linux-arm64 ./cmd/yuri_scraper/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yuri-scraper-v$(VERSION)-windows-amd64.exe ./cmd/yuri_scraper/
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yuri-scraper-v$(VERSION)-darwin-amd64 ./cmd/yuri_scraper/
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/yuri-scraper-v$(VERSION)-darwin-arm64 ./cmd/yuri_scraper/
	@echo "Release v$(VERSION) built in dist/"
	@ls -lh dist/

# Windows build (local test)
build-win:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/yuri_scraper-v$(VERSION).exe ./cmd/yuri_scraper/
	cp bin/yuri_scraper-v$(VERSION).exe bin/yuri_scraper.exe

# Quick run
run:
	go run -ldflags="$(LDFLAGS)" ./cmd/yuri_scraper/

# Clean build artifacts
clean:
	rm -rf bin/ dist/

# Download and tidy dependencies
deps:
	go mod tidy
	go mod download

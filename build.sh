#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-$(tr -d '[:space:]' < release/VERSION)}"

echo "=== YuriScraper v${VERSION} Release Builder ==="
echo ""

rm -rf dist/
mkdir -p dist/

LDFLAGS="-s -w -X main.Version=${VERSION}"

# Linux (NixOS compatible, static)
echo "Building linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/yuri-scraper-v${VERSION}-linux-amd64" ./cmd/yuri_scraper/

echo "Building linux/arm64..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/yuri-scraper-v${VERSION}-linux-arm64" ./cmd/yuri_scraper/

echo "Building windows/amd64..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/yuri-scraper-v${VERSION}-windows-amd64.exe" ./cmd/yuri_scraper/

echo "Building darwin/amd64..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/yuri-scraper-v${VERSION}-darwin-amd64" ./cmd/yuri_scraper/

echo "Building darwin/arm64..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/yuri-scraper-v${VERSION}-darwin-arm64" ./cmd/yuri_scraper/

echo ""
echo "=== Done ==="
ls -lh dist/
echo ""
echo "For NixOS: dist/yuri-scraper-v${VERSION}-linux-amd64"
echo "For Windows: dist/yuri-scraper-v${VERSION}-windows-amd64.exe"

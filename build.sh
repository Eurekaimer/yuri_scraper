#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.6.0}"

echo "=== YuriScraper v${VERSION} Release Builder ==="
echo ""

rm -rf dist/
mkdir -p dist/

LDFLAGS="-s -w -X main.Version=${VERSION}"

# Linux (NixOS compatible, static)
echo "Building linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o dist/yuri_scraper-linux-amd64 ./cmd/yuri_scraper/

echo "Building linux/arm64..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o dist/yuri_scraper-linux-arm64 ./cmd/yuri_scraper/

echo "Building windows/amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o dist/yuri_scraper-windows-amd64.exe ./cmd/yuri_scraper/

echo "Building darwin/amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o dist/yuri_scraper-darwin-amd64 ./cmd/yuri_scraper/

echo "Building darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o dist/yuri_scraper-darwin-arm64 ./cmd/yuri_scraper/

echo ""
echo "=== Done ==="
ls -lh dist/
echo ""
echo "For NixOS: dist/yuri_scraper-linux-amd64"
echo "For Windows: dist/yuri_scraper-windows-amd64.exe"

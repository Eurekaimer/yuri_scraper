[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.6.0-7C3AED?style=flat)](https://github.com/Eurekaimer/yuri_scraper/releases)
[![CI](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

# YuriScraper

> A local-first, multi-source terminal ebook downloader.

YuriScraper is a keyboard-driven Go TUI for searching, downloading, cleaning,
and exporting novels as EPUB or TXT. v0.6.0 introduces source selection for
52shuku and Yamibo, resumable chapter checkpoints, partial output on failure,
and a bounded worker pool whose concurrency can be changed while downloading.

[中文文档](README_zh.md) · [Architecture](ARCHITECTURE.md)

## Sources

| Source | Access | v0.6 support |
|---|---|---|
| 52shuku | Public | Search, metadata, chapter download |
| Yamibo | Cookie | Forum search, catalog/elevator detection, post download |

Yamibo cookies are entered inside the TUI and kept in memory only. They are not
written to YAML, history, or log files. Author-post pagination is still being
expanded; catalog/elevator threads are the recommended Yamibo path in v0.6.

## Highlights

- Source picker and in-terminal Yamibo authentication
- Fuzzy CJK result ranking
- Dynamically adjustable bounded worker pool
- Per-attempt token-bucket limiting and exponential retry backoff
- Chapter checkpoints under `novels/.cache/`
- Partial TXT/EPUB output plus a book-specific `*.failed.json` manifest
- `R` retries only missing chapters by reusing successful checkpoints
- Atomic output/history writes
- EPUB 3 metadata and Traditional-to-Simplified conversion
- Cross-platform single binaries produced by GitHub Releases

## Install

Download a binary from [GitHub Releases](https://github.com/Eurekaimer/yuri_scraper/releases),
or build from source:

```bash
git clone https://github.com/Eurekaimer/yuri_scraper.git
cd yuri_scraper
go build -trimpath -ldflags="-s -w -X main.Version=0.6.0" -o yuri-scraper ./cmd/yuri_scraper/
./yuri-scraper
```

Windows users can run the release `yuri-scraper-windows-amd64.exe` directly.

## Keyboard map

| View | Keys |
|---|---|
| Source | `1/2`, `j/k`, `Enter` |
| Authentication | paste Cookie, `Enter`, `Esc` |
| Search input | type, `Enter` search, `Tab` or `↓` results |
| Results | `j/k`, `g/G`, `Ctrl+D/U`, `Enter` download |
| Input ↔ results | `Tab` / `Shift+Tab`; `/` always focuses search |
| Progress | `1/2/3` speed; `R` retry failed chapters |
| Global | `Ctrl+S` sources, `Ctrl+L` library, `Ctrl+C` quit |

After a download, `Esc` or `Enter` returns to the existing result list instead
of discarding it or trapping focus in the input field.

## Configuration

```yaml
output_dir: novels
format: epub
concurrency: 8
rate_limit: 2.0
retry_count: 3
```

The initial values come from YAML. During a download, `1/2/3` switches both
worker concurrency and request rate. Retried requests are rate-limited too.
Yamibo applies a conservative source cap of two workers and 1.2 requests/s.

## Development

```bash
go test -short ./...
go vet ./...
make build
```

Pushing a `v*` tag builds Windows, Linux, and macOS binaries and attaches them
to a GitHub Release. Generated executables are deliberately not committed.

## Responsible use

Use the tool for personal reading organization. Keep request rates conservative,
respect site rules and copyright, and do not redistribute downloaded content.

## License

MIT

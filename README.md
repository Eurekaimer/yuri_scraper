[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.5.0-7C3AED?style=flat)](https://github.com/hzg/yuri_scraper)
[![License](https://img.shields.io/badge/MIT-green?style=flat)](LICENSE)
[![Platform](https://img.shields.io/badge/Windows%7CLinux%7CmacOS-blue?style=flat)](https://github.com/hzg/yuri_scraper/releases)

> Your terminal ebook butler — search, scrape, polish, done.

**YuriScraper** is a terminal-based TUI novel scraper for [52shuku](https://www.52shuku.net/). Fuzzy search, multi-tier concurrent downloading, EPUB/TXT output, built-in traditional-to-simplified conversion, and auto-tagging.

[中文文档](README_zh.md)

---

## Features

- **TUI** — [Bubble Tea](https://github.com/charmbracelet/bubbletea) powered, single-screen layout
- **Fuzzy search** — character overlap + N-gram + Levenshtein for accurate CJK matching
- **3 speed profiles** — slow / normal / fast, switchable mid-scrape with `1/2/3`
- **Concurrent download** — worker pool + token-bucket rate limiting
- **Metadata extraction** — author, synopsis, protagonist, tags, status
- **EPUB 3** — rich metadata (`dc:subject`, `dc:description`, protagonist)
- **Zh conversion** — auto-detect and convert traditional Chinese to simplified
- **Smart tagging** — 20+ genre patterns auto-detected from title + description
- **Failed chapter retry** — persisted to `.failed.json`, one-key retry with `R`
- **Download history** — local `.history.json`, no duplicates

---

## Quick Start

### Prerequisites

- [Go](https://go.dev/dl/) 1.26+

### Install & Run

```bash
git clone https://github.com/hzg/yuri_scraper.git
cd yuri_scraper
go mod tidy
go build -o yuri_scraper ./cmd/yuri_scraper/
./yuri_scraper
```

Or `go run ./cmd/yuri_scraper/`.

### Controls

| Context | Keys |
|---------|------|
| Search | type → `Enter` → `Tab` autocomplete |
| Results | `j/k` move `g/G` top/bottom `Ctrl+D/U` page `Enter` select |
| Progress | `1/2/3` change speed `R` retry failed |
| Library | `Ctrl+L` open `Esc` back |
| Global | `Ctrl+C` quit |

### Configuration

Edit `config.yaml`:

```yaml
output_dir: novels
format: epub            # txt or epub
concurrency: 8          # 0 = use speed profile default
rate_limit: 2.0
retry_count: 3
```

Speed profiles:

| Profile | Workers | Rate | Key |
|---------|---------|------|-----|
| Slow | 2 | 0.5 req/s | `1` |
| Normal | 8 | 2 req/s | `2` |
| Fast | 16 | 5 req/s | `3` |

---

## Development

```bash
go test -short ./...     # unit tests
go test ./...            # all tests (includes network)
go vet ./...             # lint
```

### Project Layout

```
cmd/yuri_scraper/        # entry point
internal/
  tui/                # Bubble Tea UI (single-screen)
  search/             # fuzzy search + site adapter
    sites/            # 52shuku adapter
  scraper/            # concurrent download engine
  converter/          # EPUB/TXT + zh-conv + tagger
  store/              # config + history
```

---

## Release

Prebuilt binaries at [Releases](https://github.com/hzg/yuri_scraper/releases).

```bash
make release    # cross-compile all platforms
# or ./build.sh
```

Output:

- `yuri_scraper-windows-amd64.exe`
- `yuri_scraper-linux-amd64` (NixOS compatible)
- `yuri_scraper-linux-arm64`
- `yuri_scraper-darwin-amd64`
- `yuri_scraper-darwin-arm64`

---

## License

MIT

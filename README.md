[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.6.1-7C3AED?style=flat)](https://github.com/Eurekaimer/yuri_scraper/releases)
[![CI](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

# YuriScraper

> A local-first, multi-source terminal ebook downloader.

YuriScraper is a keyboard-driven Go TUI for searching, downloading, cleaning,
and exporting novels as EPUB or TXT. v0.6.1 stabilizes source selection for
52shuku and Yamibo, resumable chapter checkpoints, partial output on failure,
and a bounded worker pool whose concurrency can be changed while downloading.

[中文文档](README_zh.md) · [Architecture](ARCHITECTURE.md)

## Sources

| Source | Access | v0.6 support |
|---|---|---|
| 52shuku | Public | Search, metadata, chapter download |
| Yamibo | Account/password | Forum search, catalog/elevator detection, post download |

Yamibo credentials are entered inside the TUI. YuriScraper reads the Discuz
`formhash`, establishes an in-memory session, and clears the input fields.
Accounts, passwords, and session cookies are not written to YAML, history,
parser checkpoints, or logs. Yamibo downloads offer explicit auto,
catalog/elevator, and paginated author-post modes.

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
- Ranked Yamibo catalog candidates with manual selection and previews
- Rate-limited, resumable Yamibo author-post pagination with PID deduplication
- Fixture-only parser tests; default tests never contact Yamibo

## Install

Download a binary from [GitHub Releases](https://github.com/Eurekaimer/yuri_scraper/releases),
or build from source:

```bash
git clone https://github.com/Eurekaimer/yuri_scraper.git
cd yuri_scraper
go build -trimpath -ldflags="-s -w -X main.Version=0.6.1" -o yuri-scraper ./cmd/yuri_scraper/
./yuri-scraper
```

Windows users can run the release `yuri-scraper-v0.6.1-windows-amd64.exe` directly.
Local Windows builds use `build.bat`, which reads `release/VERSION` and creates
only `yuri_scraper-v0.6.1.exe` in the project root. Verify it without starting
the TUI with `.\yuri_scraper-v0.6.1.exe --version`.

## Keyboard map

| View | Keys |
|---|---|
| Source | `1/2`, `j/k`, `Enter` |
| Authentication | account/password, `Tab`, `Enter`, `Esc` |
| Search input | type, `Enter` search, `Tab` or `↓` results |
| Results | `j/k`, `g/G`, `Ctrl+D/U`, `Enter` download |
| Yamibo plan | `1/2/3` mode, `Tab` fields, `h/l` catalog candidate, `Enter` continue |
| Search ↔ results | `Tab` / `Shift+Tab`, arrows, `Alt+h/l`; `/` focuses search |
| Progress | `1/2/3` speed; `R` retry failed chapters |
| Global | `Ctrl+S` sources, `Ctrl+L` library, `Ctrl+C` quit |

After a download, `Esc` or `Enter` returns to the existing result list instead
of discarding it or trapping focus in the input field. The query, complete
result set, and selected result cursor are preserved. Search and results render
as side-by-side panes on wide terminals and stack on narrow terminals.

## Configuration

```yaml
output_dir: novels
format: epub
concurrency: 8
rate_limit: 2.0
retry_count: 3

yamibo:
  max_concurrency: 2
  rate_limit: 1.2
  max_scan_pages: 10
  min_post_chars: 80
```

The initial values come from YAML. During a download, `1/2/3` switches both
worker concurrency and request rate. Retried requests are rate-limited too.
Yamibo applies a conservative source cap of two workers and 1.2 requests/s.
Existing v0.6.0 configurations remain valid; missing `yamibo` fields are
merged with these defaults. Yamibo accounts, passwords, and session cookies are
never configuration fields.

## Known limitations

- Forum markup may change; the bundled fixtures cover representative Discuz
  catalog, pagination, login-expiry, and post layouts but cannot guarantee
  every historical theme.
- Live tests require an explicit `YURISCRAPER_LIVE_TEST=1` opt-in. Default tests
  use local HTTP fixtures and never contain or read personal credentials.

## Development

```bash
go test -short ./...
go vet ./...
make build
```

Pushing a `v*` tag builds Windows, Linux, and macOS binaries and attaches them
to a GitHub Release. Artifact filenames include the version. Generated
executables are deliberately not committed.

## Responsible use

Use the tool for personal reading organization. Keep request rates conservative,
respect site rules and copyright, and do not redistribute downloaded content.

## License

MIT

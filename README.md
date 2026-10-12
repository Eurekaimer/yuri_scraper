[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.7.0-7C3AED?style=flat)](https://github.com/Eurekaimer/yuri_scraper/releases)
[![CI](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

# YuriScraper

> A local-first, multi-source terminal ebook downloader.

YuriScraper is a keyboard-driven Go application: a Bubble Tea TUI for browsing
and a scriptable command line sharing the same application service. v0.7.0
moves the download lifecycle out of the UI, adds `search`/`download`/`library`/
`config`/`install` subcommands with JSON output, streams chapters into TXT and
EPUB without holding a whole book in memory, and makes the configured
concurrency and rate limit authoritative for 52shuku.

[中文文档](README_zh.md) · [Architecture](ARCHITECTURE.md)

## Sources

| Source | Access | Support |
|---|---|---|
| 52shuku | Public | Search, metadata, chapter download |
| Yamibo | Account/password | Forum search, catalog/elevator detection, post download |

Yamibo credentials are entered inside the TUI or the credential prompt. The
Discuz `formhash` login is read, an in-memory session is established, and the
input fields are cleared. Accounts, passwords, and session cookies are never
written to YAML, history, checkpoints, logs, or JSON output.

## Install

Download a binary from [GitHub Releases](https://github.com/Eurekaimer/yuri_scraper/releases),
or install a user-level command from a source checkout without `sudo`:

```bash
git clone https://github.com/Eurekaimer/yuri_scraper.git
cd yuri_scraper
go run ./cmd/yuri_scraper/ install            # builds ~/.local/bin/yuri-scraper
```

`install` builds the checkout, atomically replaces the user command, records the
configuration path in the platform config directory, and never edits shell
startup files or `PATH`. Add the reported directory to `PATH` yourself. It
refuses to replace a non-regular file or an existing configuration.
Source installation chooses `--root`, then the embedded checkout, then the current
directory; it never infers the build checkout from an old config locator. Relative
`--config` paths are relative to the invoking working directory. A missing selected
configuration now fails explicitly instead of silently creating another library.
Source development builds report `dev`; release builds inject their version.

Build from source directly:

```bash
go build -trimpath -ldflags="-s -w -X main.Version=0.7.0" -o yuri-scraper ./cmd/yuri_scraper/
```

## Command line

Run without a command to open the TUI; `yuri-scraper tui` does the same
explicitly. Result data is written to stdout, diagnostics and progress to
stderr, so `--json` output stays pipeable.

```bash
yuri-scraper search --source 52shuku --json "书名"     # JSON result array
yuri-scraper download --source 52shuku "https://www.52shuku.net/gl/1/abc.html"
yuri-scraper library list --json
yuri-scraper open 640b3c00578a003b                    # system default reader
yuri-scraper remove 640b3c00578a003b --yes             # move file to .trash
yuri-scraper config show --json
yuri-scraper install --bin-dir ~/.local/bin --json
```

Source names accept `52shuku`/`52书库` and `yamibo`/`百合会`. `download` accepts
`--title`, `--format`, `--concurrency`, `--rate-limit`, and `--retry-count`
overrides for a single run. `--base-url` restricts 52shuku to a loopback HTTP
fixture and is rejected for every other source.

Exit codes: `0` success, `1` failure, `2` usage error, `3` partial download,
`130` canceled. `--help` and `--version` never read configuration or touch the
filesystem.

## Terminal interface and diagnostics

The Chinese workspace takes its lightweight, keyboard-driven layout cues from
[go-musicfox](https://github.com/go-musicfox/go-musicfox): a pale-yellow rounded
frame, dot-marked lists, full-row selection and fixed status/key bars. At 100
columns it can show a side-by-side preview; smaller terminals use one column.
CJK/ANSI text is measured in terminal cells, including after resize. Minimum
size is 40 × 10. `PgUp/PgDn` scroll long help, details, planning and task views.

Startup retains both artworks: a white-filled, yellow-outlined lily for 650 ms,
then the original YURI block wordmark for 950 ms. Any key skips without replaying
the key as a download. The header uses a lily emblem; no special icon font is
required. `static`, `off`, reduced motion, `NO_COLOR` and `TERM=dumb` still apply.

Failed searches keep their previous results. `r` retries a search or failed
task, `x` cancels, and `!` opens a scrollable error/recovery/log panel (`r` refresh,
`Esc` return). Pending starts are cancellable, and planning/export failures are
retryable even when no chapter failure was counted.

`<output_dir>/.diagnostics.log` contains private JSONL session/search/task events
and failed 52shuku requests. `--debug` adds successful request endpoint, status
and timing; queries, credentials, cookies, headers and bodies are not persisted.
Logs have 0600 permissions, rotate at 2 MiB with one backup, and `yuri-scraper logs`
reads at most 64 KiB / 100 recent lines. Log creation failure warns and continues
(CLI falls back to stderr; TUI keeps logs offscreen). JSON command results remain
on stdout. Live searches for `怦然为你` and `闵然` both returned
[the requested book](https://www.52shuku.net/gl/h1R2.html); Yamibo was not tested live
in this interface pass.

For agent handoff, update this file, `README_zh.md`, `ARCHITECTURE.md` and release
notes with significant behavior changes and only checks actually exercised.

## Keyboard map

The TUI has three explicit contexts: list navigation, ordinary text insertion,
and modal prompts. Text input accepts `j/k/h/l/q/:` literally.

| Context | Keys |
|---|---|
| Global (list) | `j/k` or arrows, `g g`/`G`, `Ctrl+U`/`Ctrl+D`, `?` help, `q`/`Ctrl+C` quit |
| Results | `Enter` download, `p` forum planning, `l`/`Esc` details, `Tab` preview, `/` or `i` search, `f` local filter |
| Library | `Enter`/`o` open, `v` reveal, `d` trash, `r` re-download, `/` filter, `Tab` preview |
| Search input | type, `Enter` submit, `Esc` keep the draft and return |
| Forum planning | `1/2/3` mode, `Tab` fields, `h/l` catalog candidate, `Enter` start |
| Task | `a`/`d` slower/faster, `1/2/3` preset, `x` cancel, `Esc` browse, `b` library |
| Views | `s` sources, `b` library, `t` task |

A download keeps running while you browse; the status bar shows progress on
every screen. Returning from the task view restores the previous result list,
query, and cursor.

## Configuration

```yaml
output_dir: novels
format: epub
concurrency: 8
rate_limit: 2.0
retry_count: 3

startup:
  animation: sweep      # sweep, static, or off
  reduced_motion: false

yamibo:
  max_concurrency: 2
  rate_limit: 1.2
  max_scan_pages: 10
  min_post_chars: 80
```

Relative `output_dir` resolves against the configuration file's directory, so
the library stays in one place no matter which working directory starts the
program. `--config`, then `YURI_SCRAPER_CONFIG`, then the path recorded by
`install`, then the checkout containing the running binary, then a `config.yaml`
beside the binary, then the project root or configuration file of the current
directory are tried in that order; a `config.yaml` that does not look like a
YuriScraper configuration is never adopted silently.

`concurrency` and `rate_limit` are the effective download budget, capped by any
source-declared policy. For 52shuku the configured values apply up to a sanity
ceiling of 64 concurrent requests and 20 requests/second; lower values are
preserved exactly. Yamibo additionally caps the budget at 2 workers and 1.2
requests/second. During a download, `1/2/3` replaces the startup budget with a
Slow (2 workers, 0.5 req/s), Normal (8, 2), or Fast (16, 5) profile, still
subject to the source cap. Every attempt, including retries, passes the same
token bucket.

`startup.animation` accepts `sweep`, `static`, or `off`. `reduced_motion`, a
`NO_COLOR` environment, or `TERM=dumb` disables motion as well.

## Storage

| Path | Contents |
|---|---|
| `<output_dir>/<book>-<hash>.<ext>` | finished EPUB or TXT |
| `<output_dir>/.cache/<book-hash>/` | per-chapter checkpoints |
| `<output_dir>/.history.json` | download metadata |
| `<output_dir>/<book>-<hash>.failed.json` | chapters that failed |
| `<output_dir>/.trash/` | files removed through the library |
| `<output_dir>/.library.lock` | single-writer lock |

Finished files carry a stable identity suffix derived from source and book URL,
so two books with the same title no longer collide. Interrupted downloads keep
their checkpoints; re-running the same book reuses successfully downloaded
chapters. History is written atomically and holds one entry per source/book/
format.

## Known limitations

- Forum markup may change; bundled fixtures cover representative Discuz
  catalog, pagination, login-expiry, and post layouts but cannot guarantee every
  historical theme.
- Live tests require an explicit `YURISCRAPER_LIVE_TEST=1` opt-in. Default tests
  use local HTTP fixtures and never read personal credentials.
- One download runs at a time per output directory; a second process receives a
  library-busy error instead of racing for metadata.
- Android remains a separate client. See [ARCHITECTURE.md](ARCHITECTURE.md).

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

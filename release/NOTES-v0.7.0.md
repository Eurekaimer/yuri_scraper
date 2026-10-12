# YuriScraper v0.7.0

This release moves the application out of the user interface: the same download
lifecycle now serves the terminal UI, a scriptable command line, and any future
client, and the configured request budget is actually the budget in use.

## Application service and task lifecycle

- new `internal/app` service owns planning, cancellation, history commit, and
  the single active book; the TUI no longer writes history or drives the
  scraper engine directly;
- a terminal snapshot is published exactly once and cannot be lost to progress
  backpressure, so a job that nobody watches still completes and persists;
- per-run context cancellation releases the library lock and keeps committed
  chapter checkpoints, so an interrupted download resumes from its cache;
- a second process targeting the same output directory receives an explicit
  library-busy error instead of racing for metadata.

## Command line

- subcommands: `tui`, `search`/`query`, `download`, `library`/`list`, `open`,
  `reveal`, `retry`, `remove`, `forget`, `config show`, `install`, `version`;
- results go to stdout and diagnostics/progress to stderr, with `--json`
  machine-readable output and no terminal control sequences;
- exit codes distinguish success, failure, usage errors, partial downloads, and
  cancellation; `--help` and `--version` never read configuration or create
  files;
- `install` builds a user-level `yuri-scraper` command without `sudo` and
  records the configuration path in the platform configuration directory.

## Library

- `internal/library` reconciles history with files on disk, reports missing
  files, identifies files by source plus book URL, and never deletes
  permanently: `remove` moves the reader file into `<output_dir>/.trash`;
- `open` and `reveal` use the system default application and pass the path as a
  process argument.

## Download behavior

- chapters stream into TXT and EPUB in plan order; the exporter no longer
  concatenates the whole book into memory before splitting it again;
- EPUB output is flushed per chapter so a failing destination stops the
  producer instead of losing the archive at close;
- finished files carry a stable source/book identity suffix, so identical titles
  no longer overwrite each other, and the failure manifest is per book;
- 52shuku honors the configured `concurrency` and `rate_limit` up to a sanity
  ceiling instead of replacing them with a fixed profile; Yamibo keeps its
  conservative two-worker, 1.2 request/second cap;
- a bounded HTTP transport budgets redirect hops, search, metadata, and content,
  caps decoded response bodies, and classifies retryable versus permanent
  failures, including server `Retry-After` guidance.

## Configuration and storage

- relative `output_dir` resolves against the configuration file, so the library
  no longer depends on the working directory;
- configuration resolution order is `--config`, `YURI_SCRAPER_CONFIG`, the
  installed locator, the checkout containing the binary, then the project root;
- `rate_limit` rejects NaN and infinity, and validation messages are English;
- `config show --json` exposes the effective values.

## Interface

- startup retains both artworks: a white-filled/yellow-outlined lily (650 ms),
  then the original white/yellow YURI wordmark (950 ms), both skippable;
  `static`, `off`, `reduced_motion`, `NO_COLOR`, and `TERM=dumb` still apply,
  and small terminals use a compact flower;
- list navigation, ordinary text insertion, and modal prompts are separate
  contexts, so typing `j/k/h/l/q` in a search field never moves the list;
- details are a collapsible preview rather than a separate screen, downloads
  continue while browsing, and the task view cancels without ending the program.
- a musicfox-inspired Chinese workspace uses rounded borders, dot-marked lists,
  full-row selection and fixed status/key bars; terminal-cell measurement keeps
  CJK text aligned and a 100-column breakpoint switches the preview to two panes;
- resizing reserves frame/footer space first; long help, task, planning and
  detail views support paging rather than overflowing the terminal;
- rejecting a second download preserves the active book's identity, and trash
  confirmation captures the selected entry instead of following a moving cursor.
- cancellation now reaches a not-yet-started task context, failed planning/export
  can be retried with zero failed chapters, and late search results never steal
  focus from a new draft;
- `!` opens a scrollable error/recovery/log panel; `r` retries search and failed
  tasks, `x` cancels search without throwing away the last result list.

## Diagnostics and safe startup

- CLI and TUI share private JSONL logs in `<output_dir>/.diagnostics.log`;
  `--debug` adds 52shuku request timing/status, and `logs` shows a bounded tail;
- only allowlisted failure attributes and endpoint host/path are persisted:
  no query strings, credentials, cookies, headers or bodies;
- logs rotate at 2 MiB with one backup and 0600 permissions; inability to open a
  log warns instead of preventing normal application use;
- a missing selected configuration is now explicit failure, not a silently
  recreated library; source installation no longer uses the old config locator
  as the build checkout.

## Verification limits

- parsing, ranking, scheduling, export, CLI, and library behavior are covered by
  offline fixtures and tests; `go test -short ./...`, `go vet ./...`, and
  `go build ./cmd/yuri_scraper/` pass;
- the terminal flows above were exercised against a local HTTP fixture in a real
  PTY, and the installed command was driven from a directory outside the
  checkout; real-site throughput, peak memory on very large books, and platform
  terminal differences are not benchmarked here;
- live 52shuku searches for `怦然为你` and `闵然` both returned
  `https://www.52shuku.net/gl/h1R2.html`; the 百合会 source was not exercised
  against the live site in this pass;
- log privacy, 2 MiB rotation with one backup, 0600 permissions, an unwritable
  log target that warns and continues, and a missing selected configuration that
  fails without creating another library were verified directly;
- interactive smoke runs against the local fixture covered the flower/wordmark
  startup, 40/60/100/120-column resizes, two-pane preview, Chinese search and
  download, speed presets (`慢速/标准/快速`), `x` cancel, `r` retry, error and
  log panels, and trash confirmation to an empty library;
- no CAPTCHA, Cloudflare, or access-control bypass is implemented.

## Live 52shuku corrections

A full real-site download of a 326-page book exposed several parser defects that
local fixtures could not show, because the fixtures were written from the same
assumptions as the parser:

- search results mistook the site's query highlight (`<em>`) for the author;
- the category/heat line nested inside the description block leaked into
  synopses;
- the chapter selector required `id="nr1"`, which live pages do not carry, so
  every real chapter failed extraction;
- the author's page link was emitted as a genre subject;
- the first reading page's duplicated blurb became chapter text;
- catalog entries labelled "第N页" are now normalized to `Page N`.

The same download confirmed the streaming exporter at scale: 486 chapters,
navigation identical to the manifest, no missing targets, and correct
title/author/subject metadata.

Request pacing is now randomized: a token bucket alone released requests at a
perfectly even interval, which is a detectable machine signature. Releases are
separated by at least the configured interval and at most 1.5x it, so pacing can
only be slower than the configured budget, never faster.

The task screen also accepts `a`/`d` to step one preset slower or faster next to
the existing `1`/`2`/`3` selection, and shows the effective budget after any
source cap is applied.

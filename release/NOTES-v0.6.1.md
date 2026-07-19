# YuriScraper v0.6.1

This stability release completes the offline-tested Yamibo planning workflow
without changing the 52shuku adapter.

## Added

- explicit auto, catalog/elevator, and author-post modes before Yamibo download;
- ranked catalog candidates with source node, score, chapter count, and preview;
- manual catalog candidate selection in the TUI;
- paginated author-post scanning by detected author, author name, or UID;
- configurable scan-page and minimum-body limits;
- PID deduplication, original post order, rate limiting, and resumable scan state;
- credential-free scan checkpoints and direct reuse of discovered post bodies;
- offline HTML fixtures for catalog, multi-page posts, login expiry, cleanup,
  Traditional Chinese content, short content, duplicates, and title fallback.

## Fixed

- inactive dynamic-pool workers now exit when chapter job feeding completes;
- CJK truncation now uses terminal cell width instead of rune count;
- author-search results, query text, and cursor survive a completed download;
  explicit dual panes and focus shortcuts avoid input trapping;
- v0.6.0 YAML files merge the new Yamibo defaults on load.
- all local and CI build entrypoints now read the v0.6.1 version consistently;
  binaries expose `--version` and release artifact filenames include it;
- EPUB writer failures return errors instead of panicking, and multi-site
  search no longer prints over the alternate screen.

## Security and access

- Yamibo account/password login reads the Discuz `formhash` and retains only the
  resulting session in memory. Credentials are not written to YAML, history,
  logs, errors, or scan checkpoints.
- Yamibo stays capped at two workers and 1.2 requests per second.
- No Cloudflare, CAPTCHA, or access-control bypass is implemented.

## Known limitations

- No authenticated live Yamibo session was available for this release
  candidate. Parser behavior is verified against offline fixtures only.
- Incremental page-scan counters are collected by the planner but the current
  TUI shows the strategy before download rather than live per-page updates.

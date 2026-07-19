# YuriScraper v0.6.2

This release restores a practical Yamibo workflow and keeps the existing
52shuku downloader behavior intact.

## Yamibo login and search

- account/password login reads the Discuz `formhash`, submits the standard
  login form, and retains only the resulting cookie-jar session in memory;
- credentials are cleared from the TUI and are never written to YAML, history,
  checkpoints, logs, errors, or release artifacts;
- search sends both `srchtxt` and the compatible `kw` parameter, with a
  no-`kw` fallback for alternate Discuz layouts;
- literature and translation sections can be searched together or selected
  with `Ctrl+F`;
- reply-ordered and view-ordered pages are merged by thread ID;
- reply/view counts feed a popularity score while shared CJK fuzzy relevance
  remains the primary ranking signal;
- bounded query variants improve typo and punctuation recall without issuing
  extra requests after the full query finds results;
- empty searches now show actionable feedback instead of appearing inert.

## Reliability and packaging

- the duplicate legacy ranking path was removed; every source now uses one
  stable O(n log n) fuzzy-ranking implementation;
- EPUB write failures are returned instead of panicking, and partial source
  failures no longer print over the Bubble Tea alternate screen;
- all build entrypoints read `release/VERSION`, binaries expose `--version`,
  and local Windows builds retain only the versioned executable;
- release tags are validated against `release/VERSION`; GitHub Actions runs
  tests and vet before building five platform artifacts and a SHA-256 manifest.

## Verification limits

- parser, login, section, popularity, and TUI behavior are covered by offline
  HTTP/HTML fixtures;
- no personal account file or credential is included in the repository;
- no CAPTCHA, Cloudflare, or access-control bypass is implemented.

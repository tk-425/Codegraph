# Changelog

## Unreleased

## v0.10.7 (2026-11-15)

- Registry updates are serialized by a sibling lock file and published atomically
  (same-directory temporary file, fsync, rename); concurrent `init` runs retain every
  completed registration, and interrupted saves never leave a truncated registry.
- `prune` revalidates its approved missing candidates under the update lock;
  unrelated or concurrently refreshed registrations survive.

## v0.10.6 (2026-11-13)

- Automatic Java setup passes `-data <project>/.codegraph/jdtls-workspace` per project,
  isolating generated workspace state; custom commands and explicit workspace arguments
  remain authoritative and are never silently rewritten.

## v0.10.5 (2026-11-13)

- Build and health share one effective-server resolver, including native TypeScript
  (`tsc --lsp --stdio`) selection for TypeScript 7+ projects.
- `health` reports the effective command/arguments and executable availability,
  explicitly marks initialization and capabilities as unchecked, and never starts servers.

## v0.10.4 (2026-11-12)

- Re-running `init` creates missing artifacts without rewriting an existing
  `config.toml` or `.cgignore`; writes are create-only.
- `init` reports created versus preserved paths, remains setup-only (no implicit build)
  and keeps the managed agent note idempotent.

## v0.10.3 (2026-11-12)

- `signature` preserves the full LSP `detail` string and syntax declaration headers
  (including multiline declarations and known OCaml annotations) instead of reducing
  them to a return type; missing information is labelled, never invented.
- JSON adds `documentation`, `signature_status` and `documentation_status`; human
  output labels unavailable information explicitly.
- An extractor revision participates in provider invalidation: an ordinary build
  refreshes prior extractor output even when the source bytes are unchanged.

## v0.10.2 (2026-11-12)

- `callers`/`callees` reject any `--depth` other than 1 with `not_implemented` before
  project setup; malformed depth values fail with `invalid_arguments`.
- `types` consistently reports unsupported with a nonzero exit in human and JSON modes.
- Help and tracked documentation state the one-hop and unsupported-`types` contracts.

## v0.10.1 (2026-11-11)

- Stored symbol search treats queries as literal data: SQL `%`/`_` wildcards are
  escaped, and exact-name search shares kind/language/default-module filters. Negative
  `--limit` values fail validation.
- A stored miss with `--exact` or `--kind` skips text fallback explicitly (stderr plus
  `metadata.search.skipped_tiers`) instead of fabricating symbol matches.
- Text fallback reuses the build scanner's file eligibility (default exclusions and
  `.codegraph/.cgignore` negations) and literal ripgrep matching.

## v0.10.0 (2026-11-11)

### Quality and provenance

- Add snapshot quality, build-attempt outcomes, extraction provenance and relationship
  evidence without changing existing JSON envelope keys or result-count meanings.
- Report successful publication time separately from source mtime. Preserve publication
  identity and time on newer no-op builds; leave unperformed freshness checks `unchecked`.
- Distinguish live implementation results and provider failures from stored graph results.

### Upgrade compatibility

**The v0.9.0 no-rebuild promise remains in force for format-2 indexes.** No existing
symbol identity, endpoint or stored field changes meaning. The considered format-3
transition was withdrawn because older-writer exclusion was a convenience, not a
necessary graph-format change.

Queries on an original format-2 index are read-only and do not migrate it. Optional
publication metadata is unavailable and historical relationship evidence is `unknown`
until established by a newer build. An ordinary `codegraph build`, without `--force`,
adds the reporting schema and re-extracts when trustworthy metadata is missing.

v0.9.0 remains able to read and write the extended index. Persistent database triggers
invalidate newer snapshot/attempt claims on graph, manifest or provider writes by any
writer. An older no-op build can also invalidate reports because it rewrites provider
state. Changed graph content stays queryable, but stale quality and timestamps are
not presented as current. A failed transaction rolls back invalidation; a subsequent
ordinary newer build restores trustworthy metadata. A database revision guard rejects
publication if another writer changed the stored graph during extraction, including
when its metadata was already invalid. No new hierarchy uniqueness
constraint is imposed on older inserts.

Compatibility is covered by read-only byte-preservation and SQLite invalidation/
rollback tests, plus an upgrade/downgrade/write test using the actual local v0.9.0
release source. That process test never fetches missing tags silently; CI requires
release history and fails rather than skipping the required compatibility gate.

Genuinely incompatible older identity formats still require the existing explicit,
recoverable rebuild path. Optional quality metadata alone does not require one.

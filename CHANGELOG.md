# Changelog

## Unreleased

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

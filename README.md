# CodeGraph

**CodeGraph** is a powerful, local code indexing and intelligence tool designed to help developers and AI agents understand codebases. It leverages the **Language Server Protocol (LSP)** to generate precise call graphs, symbol tables, and type hierarchies, storing them in a local SQLite database for instant querying.

> **AI-Ready**: CodeGraph is built with AI agents in mind. It provides structured "Skills" that allow agents like Claude and Gemini to navigate your code autonomously.

---

## 🚀 Features

- **Multi-Language Support**:
  - **Full Support** (LSP + Call Graph):
    - ✅ **Go** (via `gopls`)
    - ✅ **Python** (via `pyright`)
    - ✅ **TypeScript / JavaScript** (via `typescript-language-server`)
    - ✅ **React (TSX/JSX)**
    - ✅ **Rust** (via `rust-analyzer`)
    - ✅ **Java** (via `jdtls`)
  - **Partial Support** (Symbol Search only):
    - ⚠️ **Swift** (via Tree-Sitter)
    - ⚠️ **OCaml** (via Tree-Sitter)
    - ⚠️ **C, C++, C#** (via Tree-Sitter)
- **Precise Call Graphs**: Uses actual compiler/LSP data, not just regex matching.
- **Local & Offline**: All data is stored in `.codegraph/` within your project. No cloud upload.
- **Incremental Indexing**: Only re-indexes files that have changed.
- **Global Registry**: internal registry tracks all your projects (`codegraph projects`).
- **Resilient**: Falls back to Tree-Sitter or regex parsing if LSPs are unavailable.

## 📦 Installation

```bash
# Clone the repository
git clone https://github.com/tk-425/Codegraph.git
cd Codegraph

# Build the binary
go build -o codegraph ./cmd/codegraph

# Move to your PATH (optional)
mv codegraph /usr/local/bin/
```

### Upgrading from v0.9.0 (unreleased changes)

Existing **format-2 indexes remain readable without a rebuild**. Symbol identities,
call endpoints and existing stored fields keep their meanings. No `build --force`
is required for this upgrade; see the [unreleased notes](CHANGELOG.md#unreleased).

- Queries stay read-only. Before the first newer build, optional snapshot/attempt
  metadata is unavailable, historical relationship evidence is `unknown`, and
  freshness is `unchecked`. An unavailable publication time is not source mtime.
- An ordinary `codegraph build` adds optional reporting tables, evidence columns
  and persistent invalidation triggers. If trustworthy quality metadata is absent,
  that build re-extracts current inputs to establish provenance; later unchanged
  builds retain their publication ID and time.
- v0.9.0 can still read and build an extended format-2 index. Its graph, manifest
  and provider writes invalidate newer quality metadata instead of leaving stale
  claims visible. Even an older no-op build may invalidate metadata because it
  rewrites provider state. A subsequent ordinary newer build restores reporting.
  A failed publication rolls back invalidation along with graph changes. If another
  writer changes the stored graph during newer extraction, publication fails with
  an explicit retry message rather than attaching claims to a concurrent graph.
- This adds no stricter hierarchy uniqueness constraint that would reject an
  older writer's duplicate inserts. Missing historical evidence is never invented.

The previously considered format-3 bump was unnecessary: these are additive
reporting fields, not changed graph identities. Recoverable transitions remain
appropriate for genuinely incompatible formats (such as pre-format-2 indexes),
not a reason to require rebuilding compatible ones.

### Recommended Tools (LSP)

CodeGraph works best with Language Servers installed for precise call graphs and type hierarchies. If an LSP is not found, CodeGraph will **automatically fallback to Tree-sitter** for symbol extraction.

- **Go**: `go install golang.org/x/tools/gopls@latest`
- **Python**: `pip install pyright`
- **TypeScript**: install TypeScript in each project (`npm install -D typescript`). For older projects, also install `typescript-language-server` (`npm install -g typescript-language-server`). TypeScript 7+ projects use the local native LSP (`tsc --lsp --stdio`); older projects use `typescript-language-server --stdio`. Explicit `.codegraph/config.toml` commands override automatic selection.
- **Rust**: `rustup component add rust-analyzer`
- **Java**: `brew install jdtls` (macOS) or via [official setup](https://github.com/eclipse/eclipse.jdt.ls#installation)
- **Swift**: Included with Xcode (`sourcekit-lsp`)
- **OCaml**: `opam install ocaml-lsp-server`

## ⚡ Quick Start

1.  **Initialize a Project**
    Go to any coding project and run:

    ```bash
    codegraph init
    ```

    This will detect languages, create `.codegraph/config.toml`, seed `.codegraph/.cgignore` from `.gitignore` when present, and start the initial index.

    After initialization, CodeGraph uses `.codegraph/.cgignore` as the indexing policy. If you edit that file, rerun `codegraph build` to refresh the database.

2.  **Search for Symbols**
    Find functions, classes, or variables:

    ```bash
    codegraph search "authenticate"
    ```

3.  **Explore the Call Graph**
    See who calls a function:

    ```bash
    codegraph callers "authenticate"
    ```

    See what a function calls:

    ```bash
    codegraph callees "authenticate"
    ```

4.  **Check Health**
    Verify database status and LSP connections:
    ```bash
    codegraph health
    ```

## 📖 Command Reference

| Command              | Description                                                     |
| :------------------- | :-------------------------------------------------------------- |
| `init`               | Initialize codegraph in the current directory.                  |
| `build`              | Update the index (incremental). Use `--force` for full rebuild. |
| `search <query>`     | Literal symbol-name substring search, with eligible text fallback. |
| `callers <symbol>`   | Find direct callers (one hop only).                             |
| `callees <symbol>`   | Find direct callees (one hop only).                             |
| `signature <symbol>` | Show function signature and documentation.                      |
| `implementations`    | Find implementations of an interface/class.                     |
| `types <symbol>`     | Not implemented; exits nonzero in human and JSON modes.         |
| `projects`           | List all projects tracked in the global registry.               |
| `prune`              | Remove missing projects from the registry.                      |
| `health`             | Run diagnostics on the current project.                         |

Persistent flags, available on every command:

| Flag           | Description                                                                                      |
| :------------- | :----------------------------------------------------------------------------------------------- |
| `--json`       | Emit exactly one JSON envelope on stdout. Supported by `search`, `signature`, `callers`, `callees`, `implementations`, `types`, `health`, `stats`, `projects` and `build`. |
| `--lsp-stderr` | Forward raw language-server standard error instead of discarding it. Also set by `CODEGRAPH_LSP_STDERR`. |

### Search behaviour

`search` treats names as literal data: SQL `%`/`_`, regex punctuation and leading
hyphens do not become patterns or options. Put flags before `--` when a name starts
with a hyphen, for example `codegraph search --json -- '-option-like-name'`.

- `--exact` requires literal symbol-name equality, not qualified-name or method-pattern
  lookup. `--kind` and `--lang` apply to exact and substring symbol searches alike.
  Module/package declarations remain excluded unless `--kind=module` is requested.
- A stored miss falls back to literal text only when neither `--exact` nor `--kind`
  was requested. Otherwise the skip is explained on stderr and in JSON
  `metadata.search.skipped_tiers` (`tier`, `reason`); it is not an operational error.
- Text fallback uses the build scanner's supported files, default exclusions and
  `.codegraph/.cgignore`, including negations. It does not substitute ripgrep's
  `.gitignore` rules. Language filters use the scanner's stored language IDs:
  for example, `.js` is `typescript`, and `.tsx`/`.jsx` are `typescriptreact`.
- Existing case behaviour is preserved: stored substring search folds ASCII case
  (not general Unicode case), while exact name equality and text fallback are
  case-sensitive. Ripgrep configuration files cannot override the command's flags.
- `--limit` defaults to 20; zero means unlimited and negative values fail validation.
  Filters and file eligibility are applied before limiting results.

Text results remain `kind: match`, `source: ripgrep`, `origin: text` with context;
they are not symbol definitions and have no fabricated symbol ID. An empty eligible
file set needs no ripgrep process; eligible-file searches require `rg` on PATH.
Database reads do not rebuild or modify the index, and freshness remains unchecked.

### Call-query depth and unsupported types

`callers` and `callees` query one hop of the stored call graph. `--depth` defaults
to 1; explicit `--depth=1` has the same behavior. Other integer values fail with
`not_implemented` in JSON mode, before opening project state. Malformed depth
values fail with `invalid_arguments`. Human mode also exits nonzero.

`types` lookups are not implemented: every query exits nonzero, including
`--supertypes`, `--subtypes` and `--lang` combinations. JSON retains an empty
`results` array, `count: 0` and a `not_implemented` error. Neither rejection
initializes, rebuilds or modifies an index.

### JSON output

Every `--json` invocation writes one envelope with the keys `command`, `query`, `count`, `results` and `errors`; warnings and progress go to stderr. A failure exits nonzero with one entry in `errors`, including argument and flag validation. `init`, `prune` and help have no JSON form and report `json_unsupported`. Query commands open the database read-only and report `database_missing` rather than creating one. `health` reports degraded state as `ok: false` records and exits 0.

Error codes added for the output boundary: `invalid_arguments`, `json_unsupported`, `cancelled` (interrupt or `SIGTERM`), `scan_failed`, `database_init_failed`, `index_failed`, and `command_failed` for an otherwise unclassified failure. `build` results may include `request_failed` (LSP requests that failed within a phase while the build fell back) and `cleanup_failed` (a server that had to be killed) diagnostic records.

### LSP time budgets

```toml
# .codegraph/config.toml
[lsp_timeouts]
request_seconds = 30           # each ordinary LSP request
shutdown_graceful_seconds = 5  # shutdown/exit before the server is killed
shutdown_force_seconds = 5     # waiting for a killed server to be reaped
```

Values must be positive. Server initialization keeps its own 30-second budget per attempt. These are safety bounds, not performance guarantees.

## 🤖 AI Agent Integration

CodeGraph exposes **Skills** that allow AI agents to use these tools directly.

### Usage with Claude / Gemini

1.  Copy the content of `cc-skills/CODE-GRAPH.md`.
2.  Paste it into your AI session's system prompt or initial context.
3.  The agent will now know how to "Use search to find X" or "Trace the callers of Y".

See [cc-skills/README.md](cc-skills/README.md) for more details.

## 🏗️ Architecture

CodeGraph uses a **hybrid architecture**:

1.  **CLI**: Go-based entry point.
2.  **LSP Client**: Connects to language servers (std-io).
3.  **Indexer**: Extracts symbols and references.
4.  **Database**: SQLite (`.codegraph/graphs/codegraph.db`) stores the graph.
5.  **Registry**: Global JSON file (`~/.codegraph/registry.json`) tracks projects.

For a deep dive, see [.docs/Architecture.md](.docs/Architecture.md).

## 📄 License

MIT

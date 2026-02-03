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

### Recommended Tools (LSP)

CodeGraph works best with Language Servers installed for precise call graphs and type hierarchies. If an LSP is not found, CodeGraph will **automatically fallback to Tree-sitter** for symbol extraction.

- **Go**: `go install golang.org/x/tools/gopls@latest`
- **Python**: `pip install pyright`
- **TypeScript**: `npm install -g typescript-language-server typescript`
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

    This will detect languages, create `.codegraph/config.toml`, and start the initial index.

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
| `search <query>`     | Search for symbols by name (fuzzy match).                       |
| `callers <symbol>`   | Find functions that call the specified symbol.                  |
| `callees <symbol>`   | Find functions called by the specified symbol.                  |
| `signature <symbol>` | Show function signature and documentation.                      |
| `implementations`    | Find implementations of an interface/class.                     |
| `projects`           | List all projects tracked in the global registry.               |
| `prune`              | Remove missing projects from the registry.                      |
| `health`             | Run diagnostics on the current project.                         |

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

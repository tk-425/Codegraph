---
name: cg-init
description: Initialize codegraph in a project. Creates .codegraph directory, detects languages, configures LSP servers, and builds the initial symbol database. Use when setting up codegraph for the first time in a repository.
disable-model-invocation: true
allowed-tools: Bash(codegraph init)
---

# CodeGraph Initialization

Initialize codegraph in the current project. This is a one-time setup that prepares the codebase for indexing and analysis.

## What This Does

When you run this skill, it will:

1. **Create `.codegraph/` directory** - Local storage for database and config
2. **Create `config.toml`** - LSP server configurations for detected languages
3. **Create `.cgignore`** - Patterns to exclude from indexing (like .gitignore)
4. **Update `.gitignore`** - Adds `.codegraph/` to prevent committing database files
5. **Auto-detect languages** - Scans for Go, Python, TypeScript, JavaScript, Java, Rust, Swift, OCaml
6. **Run initial indexing** - Starts LSP servers and builds the symbol database

## Usage

Run the initialization command from the project root:

```bash
codegraph init
```

## What to Expect

**On first run:**
- Creates configuration files
- Detects all supported languages in your project
- Starts LSP servers (may take 5-30 seconds depending on project size)
- Indexes all symbols and builds call graph

**Example output:**
```
📁 Created .codegraph/config.toml
📁 Created .codegraph/.cgignore
📝 Added ".codegraph/" to .gitignore
🔍 Detected languages: Go, Python, TypeScript
🚀 Starting indexing...
   [gopls] Indexing 142 Go files...
   [pyright] Indexing 38 Python files...
   [typescript-language-server] Indexing 215 TypeScript files...
✅ Indexed 5,234 symbols in 12.3s
```

## When to Use

- **First time setup**: Setting up codegraph in a new repository
- **Clean slate**: After deleting `.codegraph/` directory
- **Fresh clone**: After cloning a repo that uses codegraph

## Important Notes

- **Run from project root**: Must be in the repository root directory
- **LSP requirements**: Requires language servers to be installed (gopls, pyright, etc.)
- **Time**: Initial indexing can take 10-60 seconds depending on project size
- **Registry**: Automatically registers the project in `~/.codegraph/registry.json`

## After Initialization

Once initialized, you can:
- Use `/cg-search` to find symbols
- Use `/cg-callers` to trace function usage
- Use `/cg-callees` to see dependencies
- Run `/cg-build` to rebuild after code changes

## Troubleshooting

**If initialization fails:**
1. Check that LSP servers are installed for your languages
2. Run `/cg-health` to diagnose issues
3. Check `.codegraph/` was created successfully
4. Verify you're in the project root directory

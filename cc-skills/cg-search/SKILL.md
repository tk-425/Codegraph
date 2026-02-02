---
name: cg-search
description: Search for symbols (functions, classes, variables, etc.) by name across the codebase. Supports fuzzy matching and exact search. Use when trying to locate where something is defined, find all functions matching a pattern, or discover available symbols.
argument-hint: <symbol-name> [--kind=...] [--lang=...] [--exact] [--limit=N]
allowed-tools: Bash(codegraph search*)
---

# CodeGraph Symbol Search

Search for symbols (functions, classes, variables, types, etc.) by name across your entire codebase.

## What This Does

Searches the indexed symbol database for:
- **Functions and methods**
- **Classes, structs, and interfaces**
- **Variables and constants**
- **Type definitions**
- **Modules and packages**

Returns the exact location (file and line number) where each symbol is defined.

## Basic Usage

Search for a symbol by name:
```bash
codegraph search SymbolName
```

The command uses **fuzzy matching** by default, so `codegraph search parse` will find:
- `parseConfig`
- `parseJSON`
- `JSONParser`
- `ParseHTTPRequest`

## Advanced Usage

**Filter by symbol type:**
```bash
codegraph search handler --kind=function
codegraph search User --kind=class
codegraph search API --kind=interface
```

**Filter by language:**
```bash
codegraph search Config --lang=go
codegraph search parse --lang=python,typescript
```

**Exact name matching:**
```bash
codegraph search main --exact
```

**Limit results:**
```bash
codegraph search handler --limit=10
```

**List all symbols:**
```bash
codegraph search "" --limit=100
```

**Combine filters:**
```bash
codegraph search parse --kind=function --lang=go --exact
```

## Flags Reference

| Flag | Description | Example |
|------|-------------|---------|
| `--kind` | Filter by symbol type | `--kind=function` |
| `--lang` | Filter by language(s) | `--lang=go,python` |
| `--exact` | Require exact name match | `--exact` |
| `--limit` | Max results to show | `--limit=20` |

**Available `--kind` values:**
- `function` - Functions and methods
- `class` - Classes, structs
- `interface` - Interfaces, protocols
- `variable` - Variables, constants
- `type` - Type aliases, typedefs
- `module` - Modules, packages

**Available `--lang` values:**
- `go`, `python`, `typescript`, `javascript`, `java`, `rust`, `swift`, `ocaml`

## Use Cases

**Finding a function definition:**
```bash
codegraph search handleRequest --kind=function
```

**Exploring a new codebase:**
```bash
codegraph search "" --limit=50
# Get an overview of available symbols
```

**Language-specific search:**
```bash
codegraph search auth --lang=go
# Find all Go symbols related to "auth"
```

**Finding interfaces:**
```bash
codegraph search Reader --kind=interface --lang=go
```

**Exact match for common names:**
```bash
codegraph search main --exact
# Avoid getting MainController, MainService, etc.
```

## Example Output

```
🔍 Found 3 results for 'NewManager':

  NewManager [function] (db)
    internal/db/manager.go:20
    func(dbPath string) (*Manager, error)

  NewManager [function] (db)
    internal/lsp/manager.go:22
    func(cfg *config.Config, rootURI string) *Manager

  NewManager [function] (db)
    internal/registry/registry.go:15
    func() *Manager
```

## Search Strategy

CodeGraph uses a 2-tier search approach:

1. **Database tier** (primary): Searches pre-indexed symbols - fastest and most accurate
2. **Ripgrep tier** (fallback): Text search when database has no results

Results are labeled with their source: `(db)` for database, `(ripgrep)` for fallback.

## When to Use

Use this skill when you need to:
- **Locate definitions**: "Where is `handleAuth` defined?"
- **Explore code**: "What functions are available?"
- **Find implementations**: "Show me all classes with 'Service' in the name"
- **Navigate codebase**: "Find the `User` class"
- **Verify existence**: "Does a function called `validateToken` exist?"

## Comparison with Related Skills

| Skill | Purpose | Example |
|-------|---------|---------|
| `/cg-search` | Find where symbols are defined | "Where is `parseConfig`?" |
| `/cg-callers` | Find who calls a function | "Who uses `parseConfig`?" |
| `/cg-callees` | Find what a function calls | "What does `init` call?" |

## Important Notes

- **Must be indexed**: Run `/cg-init` first to build the database
- **Rebuild after changes**: Run `/cg-build` to update after code changes
- **Case sensitivity**: Search is case-insensitive by default
- **Scope**: Searches entire project, not just current file

## Troubleshooting

**No results found:**
- Check spelling and try fuzzy search (remove `--exact`)
- Run `/cg-build` to update the database
- Use `/cg-health` to verify database is healthy

**Too many results:**
- Use `--exact` for precise matching
- Add `--kind` to filter by type
- Add `--lang` to filter by language
- Use `--limit` to see fewer results

**Symbol exists but not found:**
- Rebuild database: `/cg-build --force`
- Check if file is in `.cgignore`
- Verify language server is installed

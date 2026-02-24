---
name: codegraph
description: Symbol search and call graph analysis for specific lookups. Use when you know the exact function/variable/class name, need call chains, signatures, or implementations. Do NOT use for conceptual "where is" questions—use CodeFind instead.
---

# CodeGraph

## Overview

CodeGraph is a multi-language code indexing and call graph analysis CLI tool for intelligent code navigation. It finds symbols, traces function calls, understands dependencies, and manages project indexes. Supports Go, Python, TypeScript, JavaScript, Java, Rust, Swift, and OCaml with LSP integration.

## When to Use CodeGraph

Use CodeGraph for **specific symbol lookups** when you know the exact name:
- **"Find the X function"** - "Find the authenticate function"
- **"Who calls X?"** - "Who calls handleRequest?"
- **"What does X call?"** - "What does processPayment call?"
- **"Show me X signature"** - "What parameters does validateUser take?"
- **"What implements X?"** - "What implements the Repository interface?"
- **"Trace call chain"** - "Show me the call chain for handleError"
- **"Show project stats"** - "What's the size of this codebase?"

**Do NOT use CodeGraph for conceptual questions** where you don't know the exact symbol name — use CodeFind instead.

## When NOT to Use CodeGraph

**Use CodeFind instead if:**
- You don't know the exact symbol name
- You're asking "where is auth implemented?" (conceptual question)
- You want to find patterns or implementations across codebase
- You're exploring unfamiliar code without specific targets
- You need to compare similar implementations

Example: Don't use CodeGraph for "Where do we handle database connections?" — use CodeFind instead.

## Core Commands

### Setup & Maintenance (Manual Invoke Only)

**Initialize Project** - Creates `.codegraph/` directory and builds initial index
```bash
codegraph init
```

**Rebuild Database** - Updates symbol database after code changes
```bash
codegraph build                  # Incremental update
codegraph build --force          # Full rebuild
```

### Code Navigation (Auto-Invoked)

**Find Symbols**
```bash
codegraph search <symbol>                    # Find all matches
codegraph search <symbol> --exact            # Exact name match
codegraph search <symbol> --kind=function    # Filter by type
codegraph search <symbol> --lang=go,python   # Filter by language
```

Examples:
- "Find the authenticate function"
- "Where is the User class defined?"
- "Show me all functions with 'auth' in the name"

**Get Function Signatures**
```bash
codegraph signature <symbol>
codegraph signature <symbol> --lang=go
```

Examples:
- "What parameters does handleRequest take?"
- "What does validateUser return?"

**Find Interface Implementations**
```bash
codegraph implementations <interface>
codegraph implementations Service --lang=go
```

Examples:
- "What implements the Repository interface?"
- "Find all Service implementations"

### Call Graph Analysis (Auto-Invoked)

**Find Who Calls a Function**
```bash
codegraph callers <symbol>                  # Direct callers
codegraph callers <symbol> --depth=2        # 2 levels of callers
codegraph callers <symbol> --lang=go        # Filter by language
```

Examples:
- "Who calls authenticate?"
- "Show me the call chain to validateToken"

**Find What a Function Calls**
```bash
codegraph callees <symbol>                  # Direct callees
codegraph callees <symbol> --depth=2        # 2 levels of callees
codegraph callees <symbol> --lang=go        # Filter by language
```

Examples:
- "What does initialize call?"
- "Trace what happens in handleRequest"

### Diagnostics (Auto-Invoked)

**Show Project Statistics**
```bash
codegraph stats                       # Formatted output with sections
codegraph stats --json                # JSON output for parsing
codegraph stats --compact             # Single line summary
```

Shows symbol counts by kind, call edges, language breakdown, last build time, files indexed, and database size.

**Check Installation Health**
```bash
codegraph health
```

**List Tracked Projects**
```bash
codegraph projects
```

## Common Workflows

### Understanding New Code
```bash
codegraph search main                    # Find entry point
codegraph callees main --depth=2         # Trace execution
codegraph search Handler --kind=class    # Explore components
codegraph implementations Service        # Understand interfaces
```

### Impact Analysis (Before Refactoring)
```bash
codegraph search processPayment
codegraph signature processPayment       # Check signature
codegraph callers processPayment         # Find usage
codegraph callees processPayment         # Trace dependencies
```

### Bug Investigation
```bash
codegraph search authenticate
codegraph callers authenticate --depth=2  # Trace upstream
codegraph callees authenticate            # Trace downstream
```

## Language Support

Go, Python, TypeScript, JavaScript, Java, Rust, Swift, OCaml (with LSP integration for most)

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Search not finding symbols | Run `codegraph health` to check status, then `codegraph build` |
| Stale or missing results | Run `codegraph build` (incremental) or `codegraph build --force` (full) |
| Missing call relationships | Run `codegraph build --force` for full rebuild |
| LSP server errors | Run `codegraph health` to see which servers are missing |

## Best Practices

1. **Start broad with search** - Find the symbol first, then drill down
2. **Use depth wisely** - Start with `--depth=1`, increase only if needed
3. **Rebuild after major changes** - Keep index current with `codegraph build`
4. **Check health when troubleshooting** - Run `codegraph health` first
5. **Manual operations need explicit invocation** - `codegraph init` and `codegraph build` require user approval

## Performance

- `codegraph search`: < 1s (database lookup)
- `codegraph callers --depth=1`: < 1s (single-level)
- `codegraph callers --depth=3`: 1-3s (multi-level)
- `codegraph stats`: < 1s (database aggregation)
- `codegraph build`: 1-5s (incremental)
- `codegraph build --force`: 10-60s (full reindex)

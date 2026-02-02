---
name: cg-callers
description: Find all functions that call a specific symbol. Traces upstream dependencies to understand where and how a function is used. Use when analyzing impact of changes, understanding call chains, or finding all usages of a function.
argument-hint: <function-name> [--depth=N] [--lang=...]
allowed-tools: Bash(codegraph callers*)
---

# CodeGraph Callers Analysis

Find all functions that **call** a specific function or method. Traces upstream dependencies to show you where and how a symbol is used throughout the codebase.

## What This Does

For a given function, this skill shows:
- **Who calls it**: All functions that invoke this function
- **Where**: File path and line number of each call site
- **Context**: The calling function's signature
- **Call chains**: Multi-level call hierarchies (with `--depth`)

## Basic Usage

Find immediate callers of a function:
```bash
codegraph callers FunctionName
```

This answers: "Who calls `FunctionName`?"

## Advanced Usage

**Multi-level call chains:**
```bash
codegraph callers parseConfig --depth=2
```

Shows:
1. Functions that call `parseConfig` (depth 1)
2. Functions that call those functions (depth 2)

**Filter by language:**
```bash
codegraph callers handleRequest --lang=go
codegraph callers process --lang=python,typescript
```

**Combine options:**
```bash
codegraph callers validate --depth=3 --lang=go
```

## Flags Reference

| Flag | Description | Default | Example |
|------|-------------|---------|---------|
| `--depth` | Levels of call chain to traverse | 1 | `--depth=2` |
| `--lang` | Filter by language(s) | all | `--lang=go,python` |

## Example Output

```
📞 Callers of NewManager (5 found):

  runBuild [function]
    internal/cli/build.go:38
    func(cmd *cobra.Command, args []string) error

  runInit [function]
    internal/cli/init.go:35
    func(cmd *cobra.Command, args []string) error

  runCallers [function]
    internal/cli/callers.go:28
    func(cmd *cobra.Command, args []string) error
```

## Use Cases

### Impact Analysis
Before modifying a function, see everywhere it's used:
```bash
codegraph callers authenticate
```

If it has many callers, changes might have wide impact.

### Understanding Call Flow
Trace how a function gets invoked:
```bash
codegraph callers processPayment --depth=3
```

See the complete chain: `main` → `handleCheckout` → `validateOrder` → `processPayment`

### Finding Entry Points
See where a utility function is used:
```bash
codegraph callers formatDate
```

### Dead Code Detection
If a function has no callers, it might be unused:
```bash
codegraph callers oldLegacyFunction
# No results = potentially dead code
```

### Refactoring Safety
Before deleting or changing a function signature:
```bash
codegraph callers getUserById
```

Shows all places that need updating.

## Call Depth Explained

**Depth 1** (default): Direct callers only
```
processPayment
  ├─ handleCheckout        ← depth 1
  └─ processRefund         ← depth 1
```

**Depth 2**: Callers of callers
```
processPayment
  ├─ handleCheckout        ← depth 1
  │   └─ submitOrder       ← depth 2
  └─ processRefund         ← depth 1
      └─ handleReturn      ← depth 2
```

**Depth 3+**: Full call chains
```
processPayment
  └─ handleCheckout        ← depth 1
      └─ submitOrder       ← depth 2
          └─ main          ← depth 3
```

## Comparison with Related Skills

| Skill | Direction | Answers |
|-------|-----------|---------|
| `/cg-callers` | Upstream | "Who calls this?" |
| `/cg-callees` | Downstream | "What does this call?" |
| `/cg-search` | N/A | "Where is this defined?" |

### Visual Example

```
          cg-callers                     cg-callees
             ⬆                               ⬇
    ┌─────────────┐              ┌─────────────┐
    │   runInit   │              │             │
    │  runBuild   │──────────────│ NewManager  │─────────────▶│ Load()      │
    │ runCallers  │  calls       │             │   calls      │ Init()      │
    └─────────────┘              └─────────────┘              └─────────────┘
```

## When to Use

Use this skill when you need to:
- **Analyze impact**: "If I change this function, what breaks?"
- **Understand usage**: "How is this function being used?"
- **Trace execution**: "What calls this during startup?"
- **Find dependencies**: "What depends on this code?"
- **Refactor safely**: "Where do I need to update calls?"

## Important Notes

- **Must be indexed**: Run `/cg-init` first to build call graph
- **Rebuild after changes**: Run `/cg-build` to update after code changes
- **Function-only**: Only works for functions/methods (not variables or types)
- **Cross-file**: Finds calls across entire codebase, not just current file

## Performance

- **Depth 1**: Very fast (< 1 second)
- **Depth 2-3**: Fast (1-2 seconds)
- **Depth 4+**: May take several seconds for large codebases

## Troubleshooting

**No callers found:**
- Function might be unused (dead code)
- Run `/cg-build --force` to rebuild call graph
- Check if function is internal/private with limited scope
- Verify function exists: `/cg-search FunctionName`

**Missing some callers:**
- Rebuild database: `/cg-build --force`
- Check if calling files are in `.cgignore`
- Some languages (Swift, OCaml) have limited call graph support

**Too many results:**
- Use `--lang` to filter by language
- Use `--depth=1` to see only immediate callers
- Consider if function name is too generic

---
name: cg-callees
description: Find all functions called by a specific symbol. Traces downstream dependencies to understand what a function depends on. Use when analyzing function complexity, understanding dependencies, or mapping execution flow.
argument-hint: <function-name> [--depth=N] [--lang=...]
allowed-tools: Bash(codegraph callees*)
---

# CodeGraph Callees Analysis

Find all functions **called by** a specific function or method. Traces downstream dependencies to show what a function depends on and what it does internally.

## What This Does

For a given function, this skill shows:
- **What it calls**: All functions invoked by this function
- **Where**: File path and line number where each call happens
- **Context**: The called function's signature
- **Dependency chains**: Multi-level dependency trees (with `--depth`)

## Basic Usage

Find immediate callees of a function:
```bash
codegraph callees FunctionName
```

This answers: "What does `FunctionName` call?"

## Advanced Usage

**Multi-level dependency chains:**
```bash
codegraph callees main --depth=2
```

Shows:
1. Functions called by `main` (depth 1)
2. Functions called by those functions (depth 2)

**Filter by language:**
```bash
codegraph callees initialize --lang=go
codegraph callees setup --lang=python,typescript
```

**Combine options:**
```bash
codegraph callees startup --depth=3 --lang=go
```

## Flags Reference

| Flag | Description | Default | Example |
|------|-------------|---------|---------|
| `--depth` | Levels of call chain to traverse | 1 | `--depth=2` |
| `--lang` | Filter by language(s) | all | `--lang=go,python` |

## Example Output

```
📞 Callees of runInit (4 found):

  NewManager [function]
    internal/db/manager.go:20
    func(dbPath string) (*Manager, error)

  Load [function]
    internal/config/config.go:15
    func(path string) (*Config, error)

  Initialize [function]
    internal/lsp/manager.go:45
    func(ctx context.Context) error

  CreateDatabase [function]
    internal/db/manager.go:55
    func() error
```

## Use Cases

### Understanding Function Logic
See what a function does by examining its dependencies:
```bash
codegraph callees handleRequest
```

Shows all functions it calls to process a request.

### Complexity Analysis
Count dependencies to assess function complexity:
```bash
codegraph callees processOrder --depth=2
```

Many callees = complex function that does a lot.

### Dependency Mapping
Trace execution flow from entry points:
```bash
codegraph callees main --depth=3
```

See the complete execution tree starting from `main`.

### Refactoring Planning
Before splitting a large function, see what it depends on:
```bash
codegraph callees handleEverything
```

### Finding Bottlenecks
Identify functions that call expensive operations:
```bash
codegraph callees startup
```

Look for database queries, API calls, or heavy computations.

## Call Depth Explained

**Depth 1** (default): Direct calls only
```
main
  ├─ initialize           ← depth 1
  ├─ loadConfig           ← depth 1
  └─ startServer          ← depth 1
```

**Depth 2**: Calls made by callees
```
main
  ├─ initialize           ← depth 1
  │   ├─ connectDB        ← depth 2
  │   └─ loadPlugins      ← depth 2
  └─ startServer          ← depth 1
      └─ listenAndServe   ← depth 2
```

**Depth 3+**: Full dependency trees
```
main
  └─ initialize           ← depth 1
      └─ connectDB        ← depth 2
          ├─ openConn     ← depth 3
          └─ validateDB   ← depth 3
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
    │   runInit   │              │ Load()      │
    │  runBuild   │──────────────│ runInit     │─────────────▶│ NewManager()│
    │ runCallers  │  calls       │             │   calls      │ Initialize()│
    └─────────────┘              └─────────────┘              └─────────────┘
```

## When to Use

Use this skill when you need to:
- **Understand logic**: "What does this function actually do?"
- **Analyze complexity**: "How many dependencies does this have?"
- **Map execution**: "What gets called during initialization?"
- **Find side effects**: "What external operations does this trigger?"
- **Plan refactoring**: "What needs to move if I split this function?"

## Patterns to Look For

**Initialization Functions:**
```bash
codegraph callees init
```
Look for: configuration loading, database connections, service setup

**Handler Functions:**
```bash
codegraph callees handleRequest
```
Look for: validation, business logic, database operations, responses

**Cleanup Functions:**
```bash
codegraph callees cleanup
```
Look for: closing connections, releasing resources, saving state

## Important Notes

- **Must be indexed**: Run `/cg-init` first to build call graph
- **Rebuild after changes**: Run `/cg-build` to update after code changes
- **Function-only**: Only works for functions/methods (not variables or types)
- **Cross-file**: Finds calls across entire codebase, not just current file
- **Excludes standard library**: Typically doesn't show calls to built-in functions

## Performance

- **Depth 1**: Very fast (< 1 second)
- **Depth 2-3**: Fast (1-2 seconds)
- **Depth 4+**: May take several seconds for large codebases

## Complexity Indicators

Use callees to assess function complexity:

| Callees Count | Complexity | Recommendation |
|---------------|------------|----------------|
| 0-3 | Simple | Good, focused function |
| 4-7 | Moderate | Acceptable complexity |
| 8-15 | Complex | Consider splitting |
| 16+ | Very Complex | Definitely refactor |

## Troubleshooting

**No callees found:**
- Function might be a leaf node (doesn't call anything)
- Function might only call standard library functions
- Run `/cg-build --force` to rebuild call graph
- Verify function exists: `/cg-search FunctionName`

**Missing some callees:**
- Rebuild database: `/cg-build --force`
- Check if called files are in `.cgignore`
- Some languages (Swift, OCaml) have limited call graph support

**Too many results:**
- Use `--lang` to filter by language
- Use `--depth=1` to see only immediate calls
- Consider if function is doing too much (refactoring candidate)

## Example Analysis Session

```bash
# 1. Find a complex function
codegraph search handle --kind=function

# 2. Analyze what it does
codegraph callees handleUserRequest --depth=2

# 3. Compare with callers to see usage
codegraph callers handleUserRequest

# 4. Plan refactoring based on dependency patterns
```

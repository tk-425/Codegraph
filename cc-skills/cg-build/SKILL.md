---
name: cg-build
description: Rebuild or update the codegraph symbol database. Use after making significant code changes, switching branches, or when search results seem outdated. Supports incremental and full rebuild modes.
disable-model-invocation: true
argument-hint: [--force]
allowed-tools: Bash(codegraph build*)
---

# CodeGraph Build

Rebuild or update the symbol database to reflect current codebase state.

## What This Does

Scans your codebase and updates the symbol database with:
- **Function and method definitions**
- **Class and interface declarations**
- **Variable and constant definitions**
- **Call graph relationships** (who calls what)
- **Type hierarchy information**

## Usage

**Incremental build** (only re-indexes changed files):
```bash
codegraph build
```

**Full rebuild** (re-indexes everything from scratch):
```bash
codegraph build --force
```

## When to Use

Use this skill when:

- **After code changes**: You've added/modified/deleted functions or files
- **Branch switching**: Changed git branches with different code
- **Search misses**: `/cg-search` can't find symbols you know exist
- **Stale results**: Call graph seems outdated
- **After git pull**: Team members added new code
- **Language changes**: Added new languages to the project

## Performance

**Incremental builds** (default):
- Only re-indexes files that changed since last build
- Compares file modification times
- Much faster for small changes (1-5 seconds)
- Recommended for regular updates

**Full rebuilds** (`--force`):
- Deletes and recreates the entire database
- Re-indexes every file from scratch
- Slower (10-60 seconds depending on project size)
- Use when incremental builds miss changes

## Example Output

```
🔧 Building code graph...
   [go] 15/142 files (10%) - 234 symbols
   [python] 38/38 files (100%) - 187 symbols
   [typescript] 215/215 files (100%) - 1,023 symbols
✅ Build complete: 1,444 symbols, 318 calls in 8.2s
```

## Typical Workflow

1. Make code changes
2. Run `/cg-build` to update database
3. Use `/cg-search`, `/cg-callers`, etc. with fresh data
4. Repeat

## Important Notes

- **Must be initialized**: Run `/cg-init` first if `.codegraph/` doesn't exist
- **Working directory**: Run from anywhere in the project
- **LSP servers**: Requires language servers to be running (started automatically)
- **No file modifications**: Build only reads code, never writes to source files

## Arguments

- **No arguments**: Incremental build (default)
- **`--force`**: Full rebuild, deletes and recreates database

## Comparison: Build vs Init

| Feature | `init` | `build` |
|---------|--------|---------|
| Creates config | ✅ | ❌ |
| Creates .cgignore | ✅ | ❌ |
| Updates .gitignore | ✅ | ❌ |
| Registers project | ✅ | ❌ |
| Indexes symbols | ✅ | ✅ |
| Updates existing DB | ❌ | ✅ |
| When to use | First time | After changes |

## Troubleshooting

**Build is slow:**
- Use incremental builds (don't use `--force` unless needed)
- Check if LSP servers are responding with `/cg-health`

**Symbols not appearing:**
- Try `--force` to do a full rebuild
- Check file is not in `.cgignore` patterns
- Verify language server is installed for that file type

**"Database not initialized" error:**
- Run `/cg-init` first to initialize the project

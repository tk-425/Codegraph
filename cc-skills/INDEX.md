# CodeGraph Skills for Claude Code

A comprehensive suite of skills that enable AI agents (like Claude Code and Gemini CLI) to interact with the `codegraph` tool for code navigation, analysis, and understanding.

## Overview

CodeGraph is a multi-language code indexing and call graph analysis tool. These skills wrap the `codegraph` CLI commands with detailed instructions that teach AI models when and how to use each feature effectively.

## Available Skills

### 🔧 Setup & Maintenance

| Skill | Purpose | Manual Invoke |
|-------|---------|---------------|
| [`cg-init`](cg-init/SKILL.md) | Initialize codegraph in a project | Yes |
| [`cg-build`](cg-build/SKILL.md) | Rebuild or update the symbol database | Yes |
| [`cg-health`](cg-health/SKILL.md) | Check installation health and diagnose issues | No |
| [`cg-projects`](cg-projects/SKILL.md) | List all tracked projects | No |

### 🔍 Code Navigation

| Skill | Purpose | Manual Invoke |
|-------|---------|---------------|
| [`cg-search`](cg-search/SKILL.md) | Search for symbols by name | No |
| [`cg-signature`](cg-signature/SKILL.md) | Get function signatures and type info | No |
| [`cg-implementations`](cg-implementations/SKILL.md) | Find interface implementations | No |

### 📊 Call Graph Analysis

| Skill | Purpose | Manual Invoke |
|-------|---------|---------------|
| [`cg-callers`](cg-callers/SKILL.md) | Find who calls a function (upstream) | No |
| [`cg-callees`](cg-callees/SKILL.md) | Find what a function calls (downstream) | No |

**Manual Invoke**: Whether the skill has `disable-model-invocation: true` (you must call with `/skill-name`)

## Quick Start

### First Time Setup

```bash
# 1. Initialize codegraph in your project
/cg-init

# 2. Verify setup
/cg-health

# 3. Start exploring
Ask: "Find the main function"
Ask: "Who calls handleRequest?"
Ask: "What does initialize do?"
```

### Daily Workflow

```bash
# After making code changes
/cg-build

# Explore and analyze
/cg-search functionName
/cg-callers functionName
/cg-callees functionName
```

## Skill Categories

### When Claude Decides (Autonomous)

These skills have `disable-model-invocation: false`, meaning Claude can use them automatically when relevant:

- **`cg-search`** - Claude uses when you ask "where is X defined?"
- **`cg-callers`** - Claude uses when you ask "who calls X?"
- **`cg-callees`** - Claude uses when you ask "what does X call?"
- **`cg-signature`** - Claude uses when you ask "what's the signature of X?"
- **`cg-implementations`** - Claude uses when you ask "what implements X?"
- **`cg-health`** - Claude uses when diagnosing issues
- **`cg-projects`** - Claude uses when you ask about tracked projects

### When You Decide (Manual)

These skills have `disable-model-invocation: true`, meaning you must invoke them explicitly:

- **`/cg-init`** - Creates files and directories (side effect)
- **`/cg-build`** - Heavy operation (rebuilds database)

## Language Support

CodeGraph supports 8 programming languages with varying levels of LSP integration:

| Language | LSP Server | Status | Call Graph |
|----------|------------|--------|------------|
| **Go** | gopls | ✅ Full | ✅ Yes |
| **Python** | pyright | ✅ Full | ✅ Yes |
| **TypeScript** | typescript-language-server | ✅ Full | ✅ Yes |
| **JavaScript** | typescript-language-server | ✅ Full | ✅ Yes |
| **Java** | jdtls | ✅ Full | ✅ Yes |
| **Rust** | rust-analyzer | ✅ Full | ✅ Yes |
| **Swift** | sourcekit-lsp | ⚠️ Limited | ⚠️ Symbols only |
| **OCaml** | ocamllsp | ⚠️ Limited | ⚠️ Symbols only |

## Common Use Cases

### 1. Understanding New Codebase

```
You: "Where's the authentication logic?"
Claude: *uses /cg-search to find auth-related functions*

You: "How is login called?"
Claude: *uses /cg-callers to show call chain*

You: "What does login do?"
Claude: *uses /cg-callees to show dependencies*
```

### 2. Impact Analysis

```
You: "I want to modify handleRequest. What will break?"
Claude: *uses /cg-callers to show all usage*
Claude: *uses /cg-signature to show current signature*
```

### 3. Refactoring Planning

```
You: "This function is too complex"
Claude: *uses /cg-callees with --depth=2 to map dependencies*
Claude: *suggests split points based on call patterns*
```

### 4. Finding Implementations

```
You: "What types implement the Service interface?"
Claude: *uses /cg-implementations to list all implementations*
```

### 5. Debugging Workflow

```
You: "Search isn't finding symbols"
Claude: *uses /cg-health to diagnose*
Claude: *suggests /cg-build to rebuild database*
```

## Skill Relationships

```
                    ┌──────────────┐
                    │   cg-init    │ (Setup)
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │   cg-build   │ (Rebuild)
                    └──────┬───────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
    ┌─────▼─────┐    ┌────▼────┐    ┌─────▼──────┐
    │ cg-search │    │cg-callers│   │cg-callees │
    └───────────┘    └──────────┘    └────────────┘
          │                │                │
    ┌─────▼─────┐         │                │
    │cg-signature│         │                │
    └───────────┘          │                │
          │                │                │
    ┌─────▼────────────────▼────────────────▼──┐
    │            cg-health (Diagnose)           │
    └───────────────────────────────────────────┘
```

## Example Conversation Flows

### Flow 1: First-Time User
```
User: "I want to use codegraph in this project"
Claude: *invokes /cg-init*
Claude: "Codegraph initialized! Indexed 1,234 symbols across 3 languages."
User: "Find the main function"
Claude: *uses /cg-search main --exact --kind=function*
```

### Flow 2: Analyzing Code
```
User: "How does the authentication work?"
Claude: *uses /cg-search authenticate*
Claude: "Found authenticateUser in auth/handler.go. Let me trace its dependencies."
Claude: *uses /cg-callees authenticateUser*
Claude: "It calls: validateToken, checkPermissions, and logAccess"
```

### Flow 3: Troubleshooting
```
User: "Search isn't working"
Claude: *uses /cg-health*
Claude: "Database is 5 days old. Let me rebuild it."
Claude: *waits for manual /cg-build approval*
User: "/cg-build"
Claude: "Database rebuilt with 1,456 symbols"
```

## Installation Requirements

### CodeGraph Binary
```bash
# Install from source
git clone https://github.com/terrykang/codegraph
cd codegraph
make install
```

### Language Servers (Optional but Recommended)

```bash
# Go
go install golang.org/x/tools/gopls@latest

# Python
pip install python-lsp-server

# TypeScript/JavaScript
npm install -g typescript-language-server typescript

# Java
# Download from https://download.eclipse.org/jdtls/snapshots/

# Rust
rustup component add rust-analyzer

# Swift
# Included with Xcode

# OCaml
opam install ocaml-lsp-server
```

## Architecture

### Hybrid Registry System

- **Global Registry**: `~/.codegraph/registry.json` (tracks all projects)
- **Local Data**: `<project>/.codegraph/` (stores databases and indexes)

### Multi-Tier Search

1. **Database** - Pre-indexed symbols (fastest)
2. **Ripgrep** - Text search fallback

### Call Graph Extraction

Uses LSP `textDocument/references` for reliable call relationship extraction.

## Best Practices

### For AI Agents

1. **Check health first** when troubleshooting
2. **Use exact search** when function names are common
3. **Filter by language** to reduce noise
4. **Use depth wisely** in callers/callees (start with 1)
5. **Rebuild after major changes** to keep data fresh

### For Users

1. **Run `/cg-init` once per project**
2. **Run `/cg-build` after pulling code**
3. **Use `/cg-health` when things seem wrong**
4. **Keep LSP servers updated**
5. **Check `.cgignore` if symbols are missing**

## Troubleshooting Guide

| Problem | Check With | Solution |
|---------|------------|----------|
| Symbols not found | `/cg-health` | Run `/cg-build --force` |
| Stale results | `/cg-health` | Check last build time, rebuild if old |
| LSP errors | `/cg-health` | Verify language servers installed |
| Slow builds | `/cg-health` | Check database size, may need cleanup |
| Missing calls | `/cg-health` | Rebuild call graph with `/cg-build` |

## Contributing

To add new skills:

1. Create directory: `cc-skills/new-skill/`
2. Write `SKILL.md` with proper frontmatter
3. Test with Claude Code
4. Update this INDEX.md

## Documentation

- [Main README](../README.md)
- [Implementation Plan](../.docs/implementation-plan.md)
- [LSP Limitations](../.docs/LSP-LIMITATIONS.md)
- [Commands Reference](../.docs/Commands.md)

## Support

- **Issues**: https://github.com/terrykang/codegraph/issues
- **Docs**: https://code.claude.com/docs/en/skills
- **LSP Protocol**: https://microsoft.github.io/language-server-protocol/

---

**Version**: 1.0
**Last Updated**: 2026-02-02
**Compatible with**: Claude Code, Gemini CLI, other Agent Skills-compatible tools

# CodeGraph Skills Guide

**CodeGraph** is a multi-language code indexing and call graph analysis CLI tool. These skills enable AI agents to interact with CodeGraph for code navigation, analysis, and understanding.

## Quick Start

### First Time in a Project
```
User: "Initialize codegraph"
→ Claude invokes /cg-init
→ Creates .codegraph/ directory
→ Detects languages (Go, Python, TypeScript, etc.)
→ Indexes all symbols and builds call graph
```

### Daily Usage
```
User: "Find the authenticate function"
→ Claude uses /cg-search authenticate

User: "Who calls handleRequest?"
→ Claude uses /cg-callers handleRequest

User: "What does initialize do?"
→ Claude uses /cg-callees initialize
```

## Available Skills

### 🔧 Setup & Maintenance (Manual Invoke Only)

#### `/cg-init` - Initialize Project
**When to invoke**: First time setup in a repository
**What it does**: Creates `.codegraph/`, detects languages, builds initial index
**Example**: `/cg-init`

#### `/cg-build` - Rebuild Database
**When to invoke**: After code changes, branch switches, or when search seems stale
**What it does**: Updates symbol database (incremental or full rebuild)
**Examples**:
- `/cg-build` (incremental)
- `/cg-build --force` (full rebuild)

### 🔍 Code Navigation (Auto-Invoke)

#### `/cg-search` - Find Symbols
**Auto-invoked when**: Looking for functions, classes, variables by name
**What it provides**: Symbol definitions with file locations
**Examples**:
```
"Find the parseConfig function"
"Show me all functions with 'auth' in the name"
"Where is the User class defined?"
```

#### `/cg-signature` - Get Function Signatures
**Auto-invoked when**: Need to understand function parameters or return types
**What it provides**: Complete function signature, parameters, return types, documentation
**Examples**:
```
"What parameters does handleRequest take?"
"Show me the signature of processPayment"
"What does validateUser return?"
```

#### `/cg-implementations` - Find Interface Implementations
**Auto-invoked when**: Looking for concrete types that implement an interface
**What it provides**: All classes/structs implementing a given interface
**Examples**:
```
"What implements the Repository interface?"
"Show me all Service implementations"
"Find implementations of the Handler interface"
```

### 📊 Call Graph Analysis (Auto-Invoke)

#### `/cg-callers` - Find Who Calls a Function
**Auto-invoked when**: Tracing upstream dependencies, impact analysis
**What it provides**: All functions that call the target function
**Examples**:
```
"Who calls authenticate?"
"What functions use processPayment?"
"Show me the call chain to validateToken"
"Find callers of handleError with depth 2"
```

#### `/cg-callees` - Find What a Function Calls
**Auto-invoked when**: Understanding function dependencies, tracing execution flow
**What it provides**: All functions called by the target function
**Examples**:
```
"What does initialize call?"
"Show me the dependencies of processOrder"
"Trace what happens in handleRequest"
"What does main call with depth 3?"
```

### 🏥 Diagnostics (Auto-Invoke)

#### `/cg-health` - Check Installation Health
**Auto-invoked when**: Diagnosing issues, verifying setup
**What it provides**: Database status, symbol counts, LSP server availability, last update time
**Examples**:
```
"Check codegraph health"
"Why isn't search finding symbols?"
"Is the database up to date?"
```

#### `/cg-projects` - List Tracked Projects
**Auto-invoked when**: Managing multiple projects, checking project status
**What it provides**: All projects in global registry with status (Active/Uninitialized/Missing)
**Examples**:
```
"Show all codegraph projects"
"Which projects are indexed?"
"List tracked codebases"
```

## Common Patterns

### Pattern 1: Understanding New Code
```
1. Find entry point: "Where is main?"
2. Trace execution: "What does main call?"
3. Explore components: "Find all Handler functions"
4. Understand interfaces: "What implements Service?"
```

### Pattern 2: Impact Analysis
```
1. Find function: "Where is processPayment?"
2. Check signature: "Show processPayment signature"
3. Find usage: "Who calls processPayment?"
4. Trace dependencies: "What does processPayment call?"
```

### Pattern 3: Debugging
```
1. Locate error: "Find the authenticate function"
2. Trace upstream: "Who calls authenticate?"
3. Trace downstream: "What does authenticate call?"
4. Find similar code: "Find functions with similar patterns"
```

### Pattern 4: Refactoring
```
1. Understand current state: "Show handleRequest signature"
2. Find all callers: "Who calls handleRequest?"
3. Map dependencies: "What does handleRequest call?"
4. Plan changes based on impact
```

## Language Support

CodeGraph supports 8 languages with varying LSP integration:

| Language | Status | Call Graph | Fallback |
|----------|--------|------------|----------|
| **Go** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **Python** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **TypeScript** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **JavaScript** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **Java** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **Rust** | ✅ Full LSP | ✅ Yes | Tree-sitter |
| **Swift** | ⚠️ Limited | ⚠️ Symbols only | Tree-sitter |
| **OCaml** | ⚠️ Limited | ⚠️ Symbols only | Tree-sitter |

## How Skills Work Together

### Exploration Flow
```
/cg-search → Find symbols
    ↓
/cg-signature → Understand types
    ↓
/cg-callers ← Who uses it?
    ↓
/cg-callees ← What does it use?
    ↓
/cg-implementations → Find concrete types
```

### Maintenance Flow
```
/cg-projects → See all projects
    ↓
/cg-health → Check status
    ↓
/cg-build → Update if needed
    ↓
Ready to search and analyze
```

## Invocation Control

**Manual-Only Skills** (User must invoke):
- `/cg-init` - Creates files (side effect)
- `/cg-build` - Heavy operation (user controls timing)

**Auto-Invoke Skills** (Claude decides when to use):
- All search, analysis, and diagnostic skills
- Used when questions match their purpose

## Example Conversations

### Example 1: New Codebase Onboarding
```
You: "I'm new to this codebase. Where should I start?"

Claude: *uses /cg-search main --exact*
"The entry point is in cmd/app/main.go:20"

Claude: *uses /cg-callees main --depth=2*
"The startup sequence is:
1. initConfig → loads configuration
2. connectDatabase → establishes DB connection
3. startServer → begins HTTP server"

You: "How does authentication work?"

Claude: *uses /cg-search authenticate*
Claude: *uses /cg-callees authenticate*
"Authentication uses authenticateUser which calls:
- validateCredentials (password checking)
- loadUserFromDB (user lookup)
- generateToken (JWT creation)"
```

### Example 2: Refactoring Planning
```
You: "I need to add a parameter to processPayment"

Claude: *uses /cg-signature processPayment*
"Current signature: func processPayment(amount float64, currency string) error"

Claude: *uses /cg-callers processPayment*
"This function has 48 call sites across 4 files:
- handleCheckout (25 calls)
- processSubscription (8 calls)
- handleRefund (3 calls)
- processRecurring (12 calls)"

Claude: "Since there are many callers, I recommend a deprecation strategy:
1. Create processPaymentV2 with new parameter
2. Make old function call new one with default value
3. Migrate callers incrementally
4. Remove old function when done"
```

### Example 3: Bug Investigation
```
You: "Getting null pointer error in authenticate"

Claude: *uses /cg-search authenticate*
"Found: authenticate in internal/auth/handler.go:45"

Claude: *uses /cg-callers authenticate --depth=2*
"Call chain:
- main → handleLogin → authenticate
- main → handleAPI → authenticateToken → authenticate"

Claude: *uses /cg-callees authenticate*
"authenticate calls:
- validateToken (might return nil)
- getUserById (might return nil)"

Claude: "The null pointer likely comes from missing validation
after validateToken or getUserById. Let me check those functions..."
```

## Troubleshooting

### Search Not Finding Symbols
```
Solution: /cg-health to check status, then /cg-build to rebuild
```

### Stale Results
```
Solution: /cg-build to update database
```

### Missing Call Relationships
```
Solution: /cg-build --force to do full rebuild
```

### LSP Server Errors
```
Solution: /cg-health to check which servers are missing,
then install required language servers
```

## Tips for Using Skills

1. **Let Claude decide**: Ask natural questions, Claude will use appropriate skills
2. **Start broad**: Use search first, then drill down with callers/callees
3. **Rebuild regularly**: After major code changes, run `/cg-build`
4. **Use depth wisely**: Start with depth 1, increase if needed
5. **Check health first**: When troubleshooting, run `/cg-health`

## Architecture Notes

**Hybrid Registry System**:
- Global: `~/.codegraph/registry.json` (lightweight project index)
- Local: `<project>/.codegraph/` (SQLite database, indexes)

**Multi-Tier Search**:
1. Database (pre-indexed symbols, fastest)
2. Ripgrep (text search fallback)

**Call Graph Extraction**:
- Uses LSP `textDocument/references` for reliability
- Stored in SQLite `calls` table

## Performance Characteristics

| Operation | Time | Notes |
|-----------|------|-------|
| `/cg-search` | < 1s | Database query, very fast |
| `/cg-callers` depth=1 | < 1s | Single level lookup |
| `/cg-callers` depth=3 | 1-3s | Multiple levels |
| `/cg-build` incremental | 1-5s | Only changed files |
| `/cg-build --force` | 10-60s | Full reindex |
| `/cg-init` | 10-60s | First-time setup |

## Further Documentation

For detailed information, see:
- **INDEX.md** - Complete skill catalog with examples
- **README.md** - Installation and setup guide
- **examples/workflows/** - Step-by-step workflow guides
- **examples/TEMPLATES.md** - Templates for custom skills

## Integration with Claude Code

These skills follow the [Agent Skills](https://agentskills.io) open standard and are designed specifically for Claude Code. They:
- Use YAML frontmatter for configuration
- Support both manual and automatic invocation
- Grant appropriate tool permissions
- Provide rich descriptions for autonomous decision-making

---

**Remember**: CodeGraph reveals your codebase's structure through its call graph. Use these skills to navigate, understand, and safely modify code.

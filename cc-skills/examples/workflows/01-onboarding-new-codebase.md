# Workflow: Onboarding to a New Codebase

**Goal**: Quickly understand the structure and key components of an unfamiliar codebase.

**Time**: 15-30 minutes

**Prerequisites**:
- CodeGraph initialized in the project
- Claude Code or compatible AI tool with skills installed

---

## Phase 1: Initial Setup (2 minutes)

### Step 1: Initialize CodeGraph

If not already initialized:

```
You: "Set up codegraph in this project"
Claude: *runs /cg-init*
```

### Step 2: Verify Health

```
You: "Check codegraph health"
Claude: *runs /cg-health*
```

**Expected**: Database exists, languages detected, symbols indexed.

---

## Phase 2: Find Entry Points (5 minutes)

### Step 3: Locate Main Function

```
You: "Where is the main entry point?"
Claude: *uses /cg-search main --exact --kind=function*
```

**What to note**:
- File location
- Function signature
- Is it CLI, server, or both?

### Step 4: Trace Startup Flow

```
You: "What happens when the app starts?"
Claude: *uses /cg-callees main --depth=2*
```

**What to look for**:
- Configuration loading
- Database initialization
- Server startup
- External service connections

---

## Phase 3: Identify Core Components (10 minutes)

### Step 5: Find Key Interfaces

```
You: "Show me the main interfaces or abstract classes"
Claude: *uses /cg-search --kind=interface*
```

### Step 6: Explore Implementations

For each important interface:

```
You: "What implements [InterfaceName]?"
Claude: *uses /cg-implementations [InterfaceName]*
```

**What to note**:
- How many implementations exist
- Are they plugins, strategies, or repositories?
- Which one is the default?

### Step 7: Understand the Handler/Controller Layer

```
You: "Find all HTTP handlers" (or controllers, routes, etc.)
Claude: *uses /cg-search handler --kind=function*
```

### Step 8: Trace a Request

Pick a common endpoint:

```
You: "Show me what handleUserRequest does"
Claude: *uses /cg-callees handleUserRequest --depth=2*
```

---

## Phase 4: Explore Critical Paths (10 minutes)

### Step 9: Authentication Flow

```
You: "How does authentication work?"
Claude: *uses /cg-search auth*
Claude: *uses /cg-callees authenticate*
```

### Step 10: Data Access Layer

```
You: "Show me database interaction code"
Claude: *uses /cg-search repository --kind=interface*
Claude: *uses /cg-implementations Repository*
```

### Step 11: Error Handling

```
You: "How are errors handled?"
Claude: *uses /cg-search error --kind=function*
Claude: *uses /cg-callers handleError*
```

---

## Phase 5: Document Your Findings (5 minutes)

### Step 12: Create Mental Model

Ask Claude to summarize:

```
You: "Summarize the architecture based on what we found"
Claude: [Uses gathered information to explain structure]
```

### Step 13: Note High-Impact Areas

```
You: "What are the most-called functions?"
Claude: *uses /cg-callers on various functions to find hot paths*
```

---

## Success Criteria

After this workflow, you should know:

- ✅ Where the application starts
- ✅ How requests are processed
- ✅ Where business logic lives
- ✅ How data is accessed
- ✅ Key interfaces and their implementations
- ✅ Authentication/authorization approach
- ✅ Error handling patterns

---

## Next Steps

- **Read high-impact code**: Use `/cg-signature` to understand key functions
- **Trace specific features**: Pick a feature and follow its implementation
- **Review tests**: Look for test files to understand expected behavior
- **Check dependencies**: Use `/cg-callees` to map external service calls

---

## Common Questions During Onboarding

**"Where should I start reading?"**
```
You: "What's the most important file to read first?"
Claude: *analyzes call graph and suggests entry points*
```

**"What does this function do?"**
```
You: "Explain what [functionName] does"
Claude: *uses /cg-signature and /cg-callees to explain*
```

**"How is feature X implemented?"**
```
You: "How is [feature] implemented?"
Claude: *uses /cg-search and traces implementation*
```

**"What will break if I change this?"**
```
You: "What depends on [functionName]?"
Claude: *uses /cg-callers to show dependencies*
```

---

## Tips

1. **Start broad, go deep**: Get overview first, then dive into specifics
2. **Follow the data**: Trace how data flows through the system
3. **Use depth wisely**: Start with `--depth=1`, increase if needed
4. **Take notes**: Document your findings as you explore
5. **Ask questions**: Claude can synthesize information across multiple queries

---

## Template for Notes

```markdown
# Codebase: [Project Name]

## Entry Points
- Main: [file:line]
- Server: [file:line]
- CLI: [file:line]

## Architecture Style
- [MVC, Hexagonal, Layered, Microservices, etc.]

## Key Interfaces
- [InterfaceName]: [purpose]
  - Implementations: [list]

## Request Flow
1. [Entry point]
2. [Middleware/routing]
3. [Business logic]
4. [Data access]
5. [Response]

## Critical Components
- Authentication: [approach]
- Database: [technology and patterns]
- External Services: [list]
- Error Handling: [approach]

## Hot Paths
- [FunctionName]: [X callers, purpose]

## Notes & Gotchas
- [Important details]
```

---

**Time Check**: If you've spent 30+ minutes and still confused, ask Claude:
```
"What am I missing? Help me understand the big picture."
```

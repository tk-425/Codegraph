# CodeGraph Skills for Claude Code

[![Claude Code](https://img.shields.io/badge/Claude%20Code-Skills-blue)](https://code.claude.com/docs/en/skills)
[![Agent Skills](https://img.shields.io/badge/Agent%20Skills-Compatible-green)](https://agentskills.io)

AI-powered skills that enable Claude Code and other AI agents to interact with [CodeGraph](https://github.com/terrykang/codegraph) - a multi-language code indexing and call graph analysis tool.

## 🚀 Quick Start

### Prerequisites

1. **CodeGraph** must be installed and in your PATH
2. **Language servers** for your languages (optional but recommended)
3. **Claude Code** or another Agent Skills-compatible AI tool

### Installation

#### Option 1: Personal Skills (All Projects)

Install globally for use across all your projects:

```bash
# From the Code-Graph repository root
cp -r cc-skills/* ~/.claude/skills/
```

#### Option 2: Project-Specific Skills

Install in a specific project:

```bash
# In your project directory
mkdir -p .claude/skills
cp -r /path/to/Code-Graph/cc-skills/* ./.claude/skills/
```

#### Option 3: Symlink (Development)

Link for easier updates:

```bash
# Personal (all projects)
ln -s /path/to/Code-Graph/cc-skills/* ~/.claude/skills/

# Project-specific
mkdir -p .claude/skills
ln -s /path/to/Code-Graph/cc-skills/* ./.claude/skills/
```

### Verification

1. Start Claude Code in any project
2. Type `/` to see the slash command menu
3. Look for: `/cg-init`, `/cg-build`, `/cg-search`, etc.
4. Or ask: "What codegraph skills are available?"

## 📚 Available Skills

### Setup & Maintenance

| Skill | Command | When to Use | Auto-Invoke |
|-------|---------|-------------|-------------|
| `cg-init` | `/cg-init` | First time setup in a project | ❌ Manual only |
| `cg-build` | `/cg-build` | Rebuild after code changes | ❌ Manual only |
| `cg-health` | `/cg-health` | Diagnose issues | ✅ Yes |
| `cg-projects` | `/cg-projects` | List all tracked projects | ✅ Yes |

### Code Navigation

| Skill | Command | When to Use | Auto-Invoke |
|-------|---------|-------------|-------------|
| `cg-search` | `/cg-search <name>` | Find symbol definitions | ✅ Yes |
| `cg-signature` | `/cg-signature <name>` | Get function signature | ✅ Yes |
| `cg-implementations` | `/cg-implementations <name>` | Find interface implementations | ✅ Yes |

### Call Graph Analysis

| Skill | Command | When to Use | Auto-Invoke |
|-------|---------|-------------|-------------|
| `cg-callers` | `/cg-callers <name>` | Find who calls a function | ✅ Yes |
| `cg-callees` | `/cg-callees <name>` | Find what a function calls | ✅ Yes |

**Auto-Invoke**: Claude can use these skills automatically when your question matches their description.

## 🎓 Example Workflows

### Workflow 1: New Project Setup

**Goal**: Initialize CodeGraph in a fresh project

```
You: "Set up codegraph in this project"

Claude: *invokes /cg-init*
        Creates .codegraph/ directory
        Detects languages: Go, TypeScript
        Indexes 1,234 symbols

You: "Check if it worked"

Claude: *uses /cg-health*
        ✅ Database: 1,234 symbols
        ✅ Languages: Go (892 symbols), TypeScript (342 symbols)
        ✅ Last indexed: just now
```

### Workflow 2: Understanding New Codebase

**Goal**: Learn how authentication works

```
You: "How does authentication work in this codebase?"

Claude: *uses /cg-search authenticate*
        Found: authenticateUser, authenticateToken, validateAuth

Claude: *uses /cg-signature authenticateUser*
        func authenticateUser(username, password string) (*User, error)

Claude: "The main entry point is authenticateUser in auth/handler.go"

You: "What does it do internally?"

Claude: *uses /cg-callees authenticateUser --depth=2*
        Calls:
        ├─ validateCredentials
        │  └─ hashPassword
        ├─ loadUserFromDB
        └─ generateToken

Claude: "It validates credentials, loads user from database, and generates a JWT token."

You: "Where is it used?"

Claude: *uses /cg-callers authenticateUser*
        Called by:
        ├─ handleLogin (POST /login)
        ├─ handleAPIAuth (API middleware)
        └─ handleOAuth (OAuth callback)
```

### Workflow 3: Impact Analysis Before Refactoring

**Goal**: Safely modify a critical function

```
You: "I want to add a parameter to processPayment. What will break?"

Claude: *uses /cg-signature processPayment*
        Current: func processPayment(amount float64, currency string) error

Claude: *uses /cg-callers processPayment*
        Called by:
        ├─ handleCheckout (25 calls)
        ├─ processSubscription (8 calls)
        ├─ handleRefund (3 calls)
        └─ processRecurring (12 calls)

Claude: "This function has 48 call sites across 4 different functions.
        You'll need to update all of these callers."

You: "Show me the checkout flow"

Claude: *uses /cg-callers handleCheckout --depth=3*
        [Shows complete call chain from user action to payment]

Claude: "Here's a migration strategy:
        1. Create processPaymentV2 with new signature
        2. Update handleCheckout first (most calls)
        3. Update other callers incrementally
        4. Remove old function when all migrated"
```

### Workflow 4: Finding Interface Implementations

**Goal**: Discover all database implementations

```
You: "What database types implement the Repository interface?"

Claude: *uses /cg-implementations Repository*
        Implementations:
        ├─ PostgresRepository (db/postgres.go)
        ├─ MongoRepository (db/mongo.go)
        ├─ RedisRepository (cache/redis.go)
        └─ MockRepository (db/mock_test.go)

You: "How do they differ?"

Claude: *uses /cg-signature PostgresRepository.Save*
        *uses /cg-signature MongoRepository.Save*
        [Compares signatures and implementations]

Claude: "PostgresRepository uses SQL transactions, MongoRepository uses
        document operations. Both implement the same interface but with
        different performance characteristics."
```

### Workflow 5: Debugging Stale Results

**Goal**: Fix search not finding new code

```
You: "I added a new function but search can't find it"

Claude: *uses /cg-health*
        ⚠️ Database last updated: 3 days ago
        💡 Recommendation: Run /cg-build

You: "/cg-build"

[Rebuilds database]

Claude: "Database rebuilt: 1,456 symbols (122 added, 8 removed)"

You: "Now find my function"

Claude: *uses /cg-search newFunction*
        ✅ Found: newFunction in src/features/new.go:15
```

### Workflow 6: Exploring Call Chains

**Goal**: Trace execution from entry point

```
You: "What happens when the app starts?"

Claude: *uses /cg-search main --exact --kind=function*
        Found: main in cmd/app/main.go:20

Claude: *uses /cg-callees main --depth=3*
        main
        ├─ initConfig
        │  ├─ loadEnv
        │  └─ validateConfig
        ├─ connectDatabase
        │  ├─ openConnection
        │  └─ runMigrations
        └─ startServer
           ├─ setupRoutes
           └─ listen

Claude: "The startup sequence is:
        1. Load configuration
        2. Connect to database and run migrations
        3. Setup routes and start HTTP server"
```

## 🎯 Common Patterns

### Pattern 1: The Discovery Loop

When exploring unfamiliar code:

```
1. Search for concepts: /cg-search <concept>
2. Check signatures: /cg-signature <function>
3. Trace dependencies: /cg-callees <function>
4. Find usage: /cg-callers <function>
5. Repeat for dependencies
```

### Pattern 2: The Refactoring Checklist

Before modifying a function:

```
1. Get current signature: /cg-signature <function>
2. Find all callers: /cg-callers <function>
3. Understand dependencies: /cg-callees <function>
4. Check related implementations: /cg-implementations <interface>
5. Plan migration strategy
```

### Pattern 3: The Onboarding Script

For new team members:

```
1. Initialize: /cg-init
2. Verify health: /cg-health
3. Find entry points: /cg-search main
4. Trace startup: /cg-callees main --depth=2
5. Explore core features: Ask questions naturally
```

### Pattern 4: The Maintenance Routine

Weekly/monthly upkeep:

```
1. Check all projects: /cg-projects
2. Rebuild stale ones: /cg-build
3. Verify health: /cg-health
4. Remove missing: /cg-prune (manual command)
```

## 🛠️ Language Server Setup

### Go
```bash
go install golang.org/x/tools/gopls@latest
```

### Python
```bash
pip install python-lsp-server
# or
pip install pyright
```

### TypeScript/JavaScript
```bash
npm install -g typescript-language-server typescript
```

### Java
```bash
# Download Eclipse JDT Language Server
# https://download.eclipse.org/jdtls/snapshots/
```

### Rust
```bash
rustup component add rust-analyzer
```

### Swift
```bash
# Included with Xcode
# Verify: which sourcekit-lsp
```

### OCaml
```bash
opam install ocaml-lsp-server
```

## 🏗️ Skill Architecture

### Invocation Control

**Manual-Only Skills** (`disable-model-invocation: true`):
- `/cg-init` - Creates files (side effect)
- `/cg-build` - Heavy operation (user controls timing)

**Auto-Invoke Skills** (Claude decides when to use):
- All search, analysis, and diagnostic skills
- Used when your question matches their description

### Tool Permissions

Skills use `allowed-tools` to grant Claude specific permissions:

```yaml
# Example: cg-search grants Bash access for codegraph command
allowed-tools: Bash(codegraph search*)
```

This allows Claude to run `codegraph search` without asking permission each time.

## 📝 Custom Templates

### Template 1: Add a New Skill

Create `cc-skills/my-skill/SKILL.md`:

```yaml
---
name: my-skill
description: What this skill does and when to use it
disable-model-invocation: false  # true for manual-only
argument-hint: <required-arg> [optional-arg]
allowed-tools: Bash(codegraph *)
---

# My Skill Title

Brief description of what this skill does.

## What This Does

- Bullet point 1
- Bullet point 2

## Usage

Basic usage:
\`\`\`bash
codegraph my-command <arg>
\`\`\`

Advanced usage:
\`\`\`bash
codegraph my-command <arg> --flag=value
\`\`\`

## Example Output

\`\`\`
Expected output here
\`\`\`

## Use Cases

### Case 1: Title
Description and example

### Case 2: Title
Description and example

## When to Use

- Scenario 1
- Scenario 2

## Important Notes

- Note 1
- Note 2

## Troubleshooting

**Problem**: Description
**Solution**: Steps to fix
```

### Template 2: Workflow Document

Create a workflow guide for common tasks:

```markdown
# Workflow: [Task Name]

## Goal
What you're trying to achieve

## Prerequisites
- Requirement 1
- Requirement 2

## Steps

### 1. [Step Name]
\`\`\`bash
command here
\`\`\`
Expected outcome

### 2. [Step Name]
\`\`\`bash
command here
\`\`\`
Expected outcome

## Success Criteria
How to know you succeeded

## Troubleshooting
Common issues and fixes
```

## 🔧 Advanced Configuration

### Adjust Skill Character Budget

If you have many skills and Claude isn't seeing them all:

```bash
# Increase the character budget for skill descriptions
export SLASH_COMMAND_TOOL_CHAR_BUDGET=30000
```

### Per-Skill Customization

Edit any `SKILL.md` frontmatter:

```yaml
---
name: cg-search
description: Your custom description
model: opus  # Force specific model for this skill
context: fork  # Run in isolated subagent
agent: Explore  # Use specific subagent type
---
```

### Disable Specific Skills

Use Claude Code permissions:

```bash
# In your .claude/permissions file
# Deny specific skills
Skill(cg-init)
Skill(cg-build *)

# Allow only specific skills
Skill(cg-search)
Skill(cg-callers)
```

## 🐛 Troubleshooting

### Skills Not Appearing in Menu

**Check installation location:**
```bash
# Personal skills
ls ~/.claude/skills/

# Project skills
ls .claude/skills/

# Should see: cg-init/, cg-build/, etc.
```

**Verify SKILL.md exists:**
```bash
cat ~/.claude/skills/cg-init/SKILL.md
```

### Claude Not Using Skills Automatically

**Check description is clear:**
- Open the SKILL.md
- Ensure `description` field clearly states when to use
- Try invoking manually first: `/cg-search test`

**Check permissions:**
```bash
# See current permissions
/permissions

# Ensure Skill tool is allowed
```

### Command Not Found

**Verify codegraph is installed:**
```bash
which codegraph
codegraph version
```

**Check PATH:**
```bash
echo $PATH | grep -o "[^:]*codegraph[^:]*"
```

### Skills Work But Commands Fail

**Run health check:**
```
/cg-health
```

**Check initialization:**
```bash
# Must be in a project directory
cd /path/to/project
ls .codegraph/  # Should exist
```

## 📖 Documentation

- **[INDEX.md](INDEX.md)** - Complete skill catalog
- **[CodeGraph README](../README.md)** - Main project documentation
- **[Commands Reference](../.docs/Commands.md)** - CLI command reference
- **[Implementation Plan](../.docs/implementation-plan.md)** - Architecture details
- **[Claude Code Skills](https://code.claude.com/docs/en/skills)** - Official skills documentation

## 🤝 Contributing

### Adding New Skills

1. Create skill directory: `cc-skills/my-skill/`
2. Write `SKILL.md` following the template
3. Test with Claude Code
4. Update `INDEX.md`
5. Submit pull request

### Improving Existing Skills

1. Test the skill thoroughly
2. Identify gaps or unclear instructions
3. Update the `SKILL.md`
4. Test again with Claude Code
5. Submit pull request

## 📊 Skill Usage Statistics

Track how often skills are used (for development):

```bash
# Count skill invocations from Claude Code logs
grep -r "Skill(cg-" ~/.claude/logs/ | wc -l

# Most used skills
grep -r "Skill(cg-" ~/.claude/logs/ | \
  sed 's/.*Skill(\(cg-[^)]*\)).*/\1/' | \
  sort | uniq -c | sort -rn
```

## 🔄 Updating Skills

When CodeGraph CLI changes:

1. Update relevant `SKILL.md` files
2. Test with Claude Code
3. Update version in `INDEX.md`
4. Document changes in changelog

## 📄 License

These skills follow the same license as the CodeGraph project.

## 🙏 Acknowledgments

- Built following the [Agent Skills](https://agentskills.io) open standard
- Designed for [Claude Code](https://code.claude.com)
- Compatible with other Agent Skills-supporting AI tools

## 💬 Support

- **Issues**: https://github.com/terrykang/codegraph/issues
- **Discussions**: https://github.com/terrykang/codegraph/discussions
- **Documentation**: https://code.claude.com/docs

---

**Version**: 1.0.0
**Last Updated**: 2026-02-02
**Minimum CodeGraph Version**: 0.1.0
**Minimum Claude Code Version**: Latest

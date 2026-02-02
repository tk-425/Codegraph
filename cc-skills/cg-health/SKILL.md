---
name: cg-health
description: Check the health and status of codegraph installation. Diagnoses database status, verifies LSP servers, shows statistics, and identifies configuration issues. Use when troubleshooting, verifying setup, or checking if rebuild is needed.
allowed-tools: Bash(codegraph health)
---

# CodeGraph Health Check

Run comprehensive diagnostics on your codegraph installation and current project.

## What This Does

Checks and reports on:
- **Database status**: Existence, accessibility, corruption
- **Symbol statistics**: Count of indexed functions, classes, etc.
- **Call graph statistics**: Number of call relationships
- **Indexed languages**: Which languages are active
- **Last index time**: When database was last updated
- **LSP server availability**: Which language servers are installed
- **Configuration status**: Validity of config files

## Basic Usage

Run health check in current project:
```bash
codegraph health
```

## Example Output

### Healthy Project
```
✅ CodeGraph Health Check

📁 Project: /Users/terry/projects/my-app
   Status: Healthy

📊 Database
   Location: .codegraph/graphs/codegraph.db
   Size: 2.4 MB
   Status: ✅ Accessible

📈 Statistics
   Symbols: 5,234
     - Functions: 2,841
     - Classes: 456
     - Interfaces: 128
     - Variables: 1,809
   Calls: 3,187
   Files indexed: 432

🌐 Languages
   ✅ Go (142 files, 2,341 symbols)
   ✅ Python (38 files, 897 symbols)
   ✅ TypeScript (215 files, 1,892 symbols)
   ⚠️  Swift (12 files, 104 symbols) - Limited LSP support

⏰ Last Updated
   Built: 2026-02-02 15:23:45 (2 hours ago)
   Status: ✅ Recent

🔧 LSP Servers
   ✅ gopls (Go)
   ✅ pyright (Python)
   ✅ typescript-language-server (TypeScript)
   ❌ jdtls (Java) - Not installed
```

### Unhealthy Project
```
⚠️  CodeGraph Health Check

📁 Project: /Users/terry/projects/old-app
   Status: Issues detected

📊 Database
   Location: .codegraph/graphs/codegraph.db
   Status: ❌ Not found

   💡 Run: codegraph init

⏰ Last Updated
   Status: ⚠️  Never indexed

🔧 LSP Servers
   ✅ gopls (Go)
   ⚠️  pyright (Python) - Installed but not used
```

## Health Check Categories

### Database Status
- **✅ Healthy**: Database exists and is accessible
- **⚠️ Old**: Database exists but hasn't been updated recently (>24h)
- **⚠️ Corrupted**: Database file is damaged
- **❌ Missing**: No database found (run `/cg-init`)

### Symbol Statistics
Shows counts of:
- Total symbols indexed
- Breakdown by type (functions, classes, etc.)
- Symbols per language
- Call relationships

### Language Support
For each detected language:
- **✅ Full support**: LSP working, symbols indexed
- **⚠️ Limited**: Using fallback (tree-sitter/ripgrep)
- **❌ No support**: Language detected but no indexer available

### LSP Server Status
For each language in project:
- **✅ Available**: Server installed and working
- **⚠️ Issues**: Server installed but not responding
- **❌ Missing**: Server not installed

## Use Cases

### Troubleshooting Search Issues
If `/cg-search` isn't finding symbols:
```bash
codegraph health
```

Check:
- Is database initialized?
- When was last build?
- Are LSP servers working?

### Verifying Setup
After running `/cg-init`:
```bash
codegraph health
```

Confirm all languages detected and indexed.

### Performance Diagnosis
If codegraph is slow:
```bash
codegraph health
```

Check database size and symbol counts.

### Pre-Demo Check
Before showing codegraph to team:
```bash
codegraph health
```

Ensure everything is working properly.

### Post-Installation
After installing LSP servers:
```bash
codegraph health
```

Verify they're detected and usable.

## Common Issues and Solutions

### Database Not Found
```
❌ Database not found
```

**Solution:** Initialize the project:
```bash
codegraph init
```

### Old Database
```
⚠️ Last updated: 5 days ago
```

**Solution:** Rebuild the database:
```bash
codegraph build
```

### LSP Server Missing
```
❌ gopls (Go) - Not installed
```

**Solution:** Install the language server:
```bash
# For Go
go install golang.org/x/tools/gopls@latest

# For Python
pip install python-lsp-server

# For TypeScript
npm install -g typescript-language-server
```

### No Symbols Indexed
```
Symbols: 0
```

**Solutions:**
1. Check `.cgignore` isn't excluding everything
2. Verify language servers are installed
3. Run `codegraph build --force`

### Corrupted Database
```
❌ Database corrupted
```

**Solution:** Delete and rebuild:
```bash
rm -rf .codegraph/graphs/
codegraph init
```

## Health Indicators

### Green (✅) - Healthy
- Database exists and accessible
- Symbols indexed within last 24 hours
- All LSP servers installed and working
- No configuration errors

### Yellow (⚠️) - Warning
- Database older than 24 hours (rebuild recommended)
- Some LSP servers missing (functionality limited)
- Using fallback indexers for some languages

### Red (❌) - Critical
- Database missing or corrupted
- No symbols indexed
- Configuration errors
- All LSP servers missing

## When to Run Health Check

Run this skill:
- **After installation**: Verify setup is correct
- **Before important work**: Ensure database is fresh
- **When debugging**: Diagnose search/call graph issues
- **After code changes**: Check if rebuild is needed
- **Periodically**: Monthly maintenance check

## Important Notes

- **Non-destructive**: Health check only reads, never modifies
- **Fast**: Completes in < 1 second
- **Project-specific**: Reports on current directory's project
- **No arguments**: No configuration needed

## Reading the Output

**Symbol counts**:
- Low count: Project might need rebuild or has issues
- Zero count: Database not initialized or empty project
- High count: Large, well-indexed project

**Last updated time**:
- < 1 hour: Very fresh
- 1-24 hours: Fresh enough for most work
- > 24 hours: Consider rebuilding
- > 1 week: Definitely rebuild

**Language status**:
- All ✅: Perfect, all languages fully supported
- Some ⚠️: Some languages using fallback (still works)
- Any ❌: Missing language servers (limited functionality)

## Example Diagnostic Session

```bash
# 1. Check health
codegraph health

# 2. If database is old, rebuild
codegraph build

# 3. Verify rebuild worked
codegraph health

# 4. Test search
codegraph search main --limit=5

# 5. If search fails, force rebuild
codegraph build --force
codegraph health
```

## Comparison with Related Skills

| Skill | Purpose |
|-------|---------|
| `/cg-health` | Diagnose issues, check status |
| `/cg-init` | Initial setup |
| `/cg-build` | Fix stale database |
| `/cg-projects` | List all tracked projects |

## Troubleshooting

**Health command not found:**
- Verify codegraph is installed: `which codegraph`
- Check PATH includes codegraph binary
- Reinstall if necessary

**Permission errors:**
- Check `.codegraph/` directory permissions
- Ensure you own the project directory
- Run from project root, not subdirectory

**Confusing output:**
- Health check shows current project only
- Use `/cg-projects` to see all tracked projects
- Change directory to project root if needed

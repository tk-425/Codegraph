---
name: cg-projects
description: List all projects tracked in the codegraph global registry. Shows project paths, names, and status (Active, Uninitialized, or Missing). Use when managing multiple projects, verifying which projects are indexed, or before switching between codebases.
allowed-tools: Bash(codegraph projects)
---

# CodeGraph Projects Registry

List all projects tracked in the global codegraph registry with their current status.

## What This Does

Shows a table of all projects that have been initialized with codegraph:
- **Project name**
- **Full file path**
- **Current status** (Active, Uninitialized, or Missing)
- **Last seen timestamp**

The registry is stored globally at `~/.codegraph/registry.json` and tracks all projects across your system.

## Basic Usage

List all tracked projects:
```bash
codegraph projects
```

## Example Output

```
📋 CodeGraph Projects

┌─────────────────┬──────────────────────────────────────┬──────────────────┬─────────────────────┐
│ Name            │ Path                                 │ Status           │ Last Seen           │
├─────────────────┼──────────────────────────────────────┼──────────────────┼─────────────────────┤
│ Code-Graph      │ /Users/terry/projects/code-graph     │ ✅ Active        │ 2026-02-02 15:30:00 │
│ my-api          │ /Users/terry/work/my-api             │ ✅ Active        │ 2026-02-02 14:15:00 │
│ old-frontend    │ /Users/terry/archive/old-frontend    │ ⚠️  Uninitialized│ 2025-12-15 09:00:00 │
│ deleted-project │ /Users/terry/temp/deleted-project    │ ⚠️  MISSING      │ 2025-11-20 16:45:00 │
└─────────────────┴──────────────────────────────────────┴──────────────────┴─────────────────────┘

Total: 4 projects (2 active, 1 uninitialized, 1 missing)
```

## Project Status Explained

### ✅ Active
- Project directory exists
- `.codegraph/` directory exists
- Database is present and accessible
- **Ready to use** all codegraph commands

### ⚠️ Uninitialized
- Project directory exists
- `.codegraph/` directory is missing
- **Action needed:** Run `codegraph init` in that directory

### ⚠️ MISSING
- Project directory no longer exists on disk
- Was deleted, moved, or on unmounted drive
- **Action needed:** Remove from registry with `/cg-prune` (manual task)

## Use Cases

### Project Overview
See all codegraph-enabled projects on your system:
```bash
codegraph projects
```

### Verify Initialization
Check if a project is properly initialized:
```bash
codegraph projects
# Look for your project in the list and check its status
```

### Before Switching Projects
Confirm a project is ready before working on it:
```bash
codegraph projects
# Ensure status is "Active" before cd'ing
```

### Audit Indexed Codebases
Review all projects you've set up with codegraph:
```bash
codegraph projects
```

### Find Stale Projects
Identify projects that need reinitialization:
```bash
codegraph projects
# Look for "Uninitialized" status
```

## Understanding Last Seen

The "Last Seen" timestamp shows when codegraph last interacted with the project:
- **Updated on**: `init`, `build`, `search`, `callers`, `callees`, etc.
- **Recent**: < 1 day - actively used
- **Stale**: 1-30 days - occasionally used
- **Old**: > 30 days - rarely used

## Registry Lifecycle

### Projects Added To Registry
Automatically added when you run:
```bash
codegraph init
```

### Projects Updated In Registry
Last seen timestamp updated when you run any codegraph command in that project.

### Projects Removed From Registry
**NOT automatic.** You must manually prune:
```bash
codegraph prune
```

## Typical Workflow

**1. Check all projects:**
```bash
codegraph projects
```

**2. Navigate to a project:**
```bash
cd /path/to/project
```

**3. Verify status:**
- If Active: Ready to use
- If Uninitialized: Run `codegraph init`
- If Missing: Project was deleted

**4. Use codegraph:**
```bash
codegraph search FunctionName
```

## Multi-Project Scenario

If you work on multiple codebases:

```bash
# Morning: Check what's available
codegraph projects

# Work on project A
cd ~/projects/api-service
codegraph search handleRequest

# Switch to project B
cd ~/projects/frontend
codegraph callers fetchData

# End of day: All projects still tracked
codegraph projects
```

## Status Transitions

### Active → Uninitialized
Happens when you delete `.codegraph/` directory:
```bash
rm -rf .codegraph/
codegraph projects  # Shows "Uninitialized"
```

**Fix:** Run `codegraph init` again

### Active → Missing
Happens when you delete or move the project:
```bash
rm -rf /path/to/project
codegraph projects  # Shows "MISSING"
```

**Fix:** Prune the entry with `codegraph prune`

### Uninitialized → Active
Run `codegraph init`:
```bash
cd /path/to/project
codegraph init
codegraph projects  # Shows "Active"
```

## Global vs Local Data

### Global Registry (`~/.codegraph/registry.json`)
- **What**: Lightweight "phone book" of projects
- **Stores**: Project paths, names, timestamps
- **Size**: Tiny (few KB)
- **Purpose**: Track where projects are

### Local Data (`<project>/.codegraph/`)
- **What**: Heavy database files
- **Stores**: Symbols, call graphs, indexes
- **Size**: Large (1-100 MB)
- **Purpose**: Store indexed code data

**Philosophy**:
- Registry is global and lightweight
- Heavy data stays local to each project
- Deleting a project deletes its data

## Comparison with Related Skills

| Skill | Scope | Purpose |
|-------|-------|---------|
| `/cg-projects` | All projects | List tracked projects |
| `/cg-health` | Current project | Check database status |
| `/cg-init` | Current project | Initialize new project |

## When to Use

Use this skill when you:
- **Start your day**: See available projects
- **Switch projects**: Check if target is Active
- **Audit setup**: Review all indexed codebases
- **Debug issues**: Verify project is tracked
- **Clean up**: Find Missing projects to prune

## Important Notes

- **Read-only**: This command only displays, never modifies
- **Fast**: Completes instantly (< 100ms)
- **Global view**: Shows all projects, not just current one
- **No filtering**: Shows all tracked projects (use grep if needed)

## Filtering Output

Use shell tools to filter:

**Find specific project:**
```bash
codegraph projects | grep my-api
```

**Show only active:**
```bash
codegraph projects | grep "✅ Active"
```

**Show only problems:**
```bash
codegraph projects | grep -E "(Uninitialized|MISSING)"
```

**Count projects:**
```bash
codegraph projects | grep -c "Active"
```

## Example Scenarios

### Scenario 1: New Machine Setup
```bash
# 1. Check what's tracked
codegraph projects
# Output: Empty (fresh machine)

# 2. Initialize first project
cd ~/projects/my-app
codegraph init

# 3. Verify it's tracked
codegraph projects
# Output: Shows my-app as Active
```

### Scenario 2: Spring Cleaning
```bash
# 1. See all projects
codegraph projects

# 2. Note MISSING projects

# 3. Clean up registry
codegraph prune

# 4. Verify cleaned
codegraph projects
```

### Scenario 3: Team Onboarding
```bash
# 1. Clone team repos
git clone <repo1>
git clone <repo2>

# 2. Initialize codegraph in each
cd repo1 && codegraph init
cd ../repo2 && codegraph init

# 3. Verify all tracked
codegraph projects
# Should show both repos as Active
```

## Troubleshooting

**No projects shown:**
- You haven't initialized any projects yet
- Run `/cg-init` in a project directory first
- Check `~/.codegraph/registry.json` exists

**Wrong status shown:**
- Status is computed in real-time
- If showing Uninitialized but `.codegraph/` exists, try `codegraph build`
- If showing MISSING but folder exists, check absolute path

**Registry corrupted:**
- Backup: `cp ~/.codegraph/registry.json ~/registry-backup.json`
- Delete and reinitialize projects
- Registry will be recreated automatically

**Projects not updating:**
- Last Seen only updates when you run codegraph commands
- Running commands in subdirectories still counts
- Read-only commands (like this one) don't update timestamps

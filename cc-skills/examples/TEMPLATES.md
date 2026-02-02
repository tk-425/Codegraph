# CodeGraph Skills Templates

Quick-start templates for common tasks and custom skill creation.

---

## Template 1: Create a New Skill

Use this template to add new skills to the cc-skills directory.

### File: `cc-skills/my-skill/SKILL.md`

```yaml
---
name: my-skill
description: Clear description of what this skill does and when to use it. Be specific about scenarios so Claude knows when to invoke it automatically.
disable-model-invocation: false  # Set to true if user must invoke manually
argument-hint: <required> [optional]  # Shows in autocomplete
allowed-tools: Bash(codegraph *)  # Tools Claude can use without asking
model: sonnet  # Optional: force specific model (sonnet/opus/haiku)
context: fork  # Optional: run in isolated subagent
agent: Explore  # Optional: which subagent type (with context: fork)
---

# Skill Title

One-sentence description of what this skill does.

## What This Does

Clear bullet points:
- What information it provides
- What operations it performs
- What output it generates

## Basic Usage

Simplest way to use this skill:

\`\`\`bash
codegraph command <args>
\`\`\`

## Advanced Usage

More complex patterns with flags:

\`\`\`bash
codegraph command <args> --flag1 --flag2=value
\`\`\`

## Example Output

\`\`\`
Show what users will see when this runs
Include realistic data
\`\`\`

## Use Cases

### Use Case 1: [Scenario Name]

**When**: Description of scenario

**How**:
\`\`\`bash
codegraph command example
\`\`\`

**Result**: What you learn or accomplish

### Use Case 2: [Scenario Name]

**When**: Description of scenario

**How**:
\`\`\`bash
codegraph command example
\`\`\`

**Result**: What you learn or accomplish

## When to Use

Bullet list of situations:
- When you need to...
- When debugging...
- When exploring...
- Before/after...

## Flags Reference

| Flag | Description | Default | Example |
|------|-------------|---------|---------|
| `--flag1` | What it does | value | `--flag1=example` |
| `--flag2` | What it does | value | `--flag2=example` |

## Comparison with Related Skills

| Skill | Purpose | When to Use |
|-------|---------|-------------|
| `/my-skill` | This skill | [scenario] |
| `/other-skill` | Related | [scenario] |

## Important Notes

- Note about requirements
- Note about prerequisites
- Note about limitations
- Note about performance

## Troubleshooting

**Problem**: Common issue description

**Solution**: Steps to fix
\`\`\`bash
commands to run
\`\`\`

**Problem**: Another common issue

**Solution**: Steps to fix

## Related Skills

- `/skill-1` - Brief description
- `/skill-2` - Brief description
```

---

## Template 2: Workflow Document

Use this for documenting common task patterns.

### File: `cc-skills/examples/workflows/my-workflow.md`

```markdown
# Workflow: [Task Name]

**Goal**: What you're trying to achieve

**Time**: Estimated duration

**Prerequisites**:
- Requirement 1
- Requirement 2
- Requirement 3

---

## Phase 1: [Phase Name] (X minutes)

### Step 1: [Step Name]

Description of what to do.

\`\`\`
User: "What to say to Claude"
Claude: *what Claude does*
\`\`\`

**Expected**: What should happen

**Document**: What to note or save

### Step 2: [Step Name]

Description of next step.

\`\`\`
User: "Next instruction"
Claude: *Claude's action*
\`\`\`

---

## Phase 2: [Phase Name] (X minutes)

### Step 3: [Step Name]

Continue the pattern...

---

## Success Criteria

How to know you succeeded:

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3

---

## Next Steps

What to do after completing this workflow:

1. Follow-up action 1
2. Follow-up action 2

---

## Common Issues

**Issue**: Problem that might occur

**Solution**: How to fix it

---

## Tips

1. Tip 1
2. Tip 2
3. Tip 3
```

---

## Template 3: Conversation Script

Use this for training or demonstration purposes.

```markdown
# Conversation: [Scenario Name]

**Goal**: [What this demonstrates]

**Duration**: [Time estimate]

---

## Setup

Prerequisites or context needed.

---

## Conversation Flow

**User**: "Initial question or request"

**Claude**: *uses /skill-name*

"Claude's response explaining what was found"

---

**User**: "Follow-up question"

**Claude**: *uses /another-skill*

"More detailed response"

---

**User**: "Request for specific action"

**Claude**: "Explanation of what will be done"

*uses /skill-name with args*

"Confirmation of action"

---

## Outcome

What was learned or accomplished.

---

## Variations

### Variation 1: [Different approach]

How the conversation might go differently.

### Variation 2: [Another approach]

Alternative path to same goal.
```

---

## Template 4: Quick Reference Card

Use for creating skill cheat sheets.

```markdown
# [Skill Name] Quick Reference

## One-Line Summary
Brief description of what it does.

## Quick Start
\`\`\`bash
codegraph command <args>
\`\`\`

## Common Patterns

**Pattern 1**: [Name]
\`\`\`bash
codegraph command pattern1
\`\`\`

**Pattern 2**: [Name]
\`\`\`bash
codegraph command pattern2
\`\`\`

## Flags
- `--flag1`: Description
- `--flag2`: Description

## When to Use
- Scenario 1
- Scenario 2

## Examples

### Example 1: [Task]
\`\`\`bash
codegraph command example1
\`\`\`

### Example 2: [Task]
\`\`\`bash
codegraph command example2
\`\`\`

## Tips
- Tip 1
- Tip 2

## Related
- `/skill1` - Brief
- `/skill2` - Brief
```

---

## Template 5: Troubleshooting Guide

Use for documenting common problems and solutions.

```markdown
# Troubleshooting: [Skill Name]

## Problem: [Issue Description]

**Symptoms**:
- Symptom 1
- Symptom 2

**Diagnosis**:
\`\`\`bash
# Commands to run to diagnose
codegraph health
codegraph command --debug
\`\`\`

**Solution**:

### Method 1: [Approach]
\`\`\`bash
# Steps to fix
command1
command2
\`\`\`

### Method 2: [Alternative]
\`\`\`bash
# Alternative approach
command1
command2
\`\`\`

**Prevention**:
How to avoid this problem in the future.

---

## Problem: [Another Issue]

[Repeat the pattern]
```

---

## Template 6: Integration Guide

Use for documenting how to integrate CodeGraph skills with other tools.

```markdown
# Integration: [Tool Name] + CodeGraph

## Overview

How CodeGraph skills enhance [Tool Name].

## Setup

### Prerequisites
- Requirement 1
- Requirement 2

### Installation
\`\`\`bash
installation commands
\`\`\`

### Configuration
\`\`\`bash
configuration commands
\`\`\`

## Usage Patterns

### Pattern 1: [Use Case]

**Scenario**: Description

**Workflow**:
1. Use [Tool Name] to...
2. Use CodeGraph to...
3. Return to [Tool Name] to...

**Example**:
\`\`\`bash
tool command
codegraph command
tool command
\`\`\`

### Pattern 2: [Another Use Case]

[Repeat pattern]

## Tips

1. Tip about integration
2. Tip about workflow
3. Tip about gotchas

## Examples

### Example 1: [Scenario]
Detailed walkthrough

### Example 2: [Scenario]
Detailed walkthrough
```

---

## Template 7: Learning Path

Use for creating structured learning materials.

```markdown
# Learning Path: [Topic]

## Level 1: Beginner (30 minutes)

**Goal**: Basic competency with core skills

### Lesson 1: [Skill Name]
- Read: [link to skill docs]
- Practice: [exercise]
- Success: [criteria]

### Lesson 2: [Next Skill]
- Read: [link]
- Practice: [exercise]
- Success: [criteria]

## Level 2: Intermediate (1 hour)

**Goal**: Combine skills for common tasks

### Lesson 3: [Workflow]
- Follow: [workflow document]
- Practice: [exercise]
- Success: [criteria]

## Level 3: Advanced (2 hours)

**Goal**: Complex analysis and optimization

### Lesson 4: [Advanced Topic]
- Explore: [concept]
- Practice: [complex exercise]
- Success: [criteria]

## Certification Challenge

Complete this task to demonstrate mastery:

**Task**: [Description]

**Requirements**:
- [ ] Requirement 1
- [ ] Requirement 2

**Time Limit**: [duration]

**Success Criteria**:
- Metric 1
- Metric 2
```

---

## Template 8: Release Notes

Use when updating skills with new features.

```markdown
# Release Notes: [Skill Name] v[X.Y.Z]

**Release Date**: YYYY-MM-DD

## New Features

### Feature 1: [Name]

**What it does**: Description

**How to use**:
\`\`\`bash
codegraph new-feature example
\`\`\`

**Example**:
\`\`\`
output example
\`\`\`

## Improvements

- Improvement 1
- Improvement 2

## Bug Fixes

- Fix 1
- Fix 2

## Breaking Changes

### Change 1: [Description]

**Before**:
\`\`\`bash
old syntax
\`\`\`

**After**:
\`\`\`bash
new syntax
\`\`\`

**Migration**:
Steps to update existing usage.

## Deprecations

- Feature X deprecated (remove in v[X+1])
  - Use Feature Y instead
  - Migration guide: [link]

## Known Issues

- Issue 1 (workaround: [description])
- Issue 2 (fix planned for v[X+1])
```

---

## Using These Templates

1. **Copy template** to appropriate location
2. **Fill in sections** with your content
3. **Test with Claude Code** to ensure it works
4. **Update INDEX.md** if adding new skills
5. **Submit PR** if contributing back

## Template Customization

Feel free to:
- Add sections relevant to your use case
- Remove sections that don't apply
- Adjust formatting to match your style
- Combine templates for complex documentation

## Best Practices

1. **Keep descriptions clear**: Claude uses them to decide when to invoke
2. **Include examples**: Real-world examples are most helpful
3. **Test before documenting**: Ensure commands actually work
4. **Update regularly**: Keep documentation in sync with code
5. **Cross-reference**: Link related skills and workflows

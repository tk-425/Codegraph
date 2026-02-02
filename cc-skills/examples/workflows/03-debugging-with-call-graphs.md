# Workflow: Debugging with Call Graphs

**Goal**: Use call graph analysis to identify bug sources and trace execution flow.

**Time**: 5-15 minutes per bug

**Prerequisites**:
- Bug report or error description
- CodeGraph database up to date

---

## Phase 1: Locate the Problem (2 minutes)

### Step 1: Find the Error Location

**From error message**:
```
Error: "Invalid token in authenticate()"
```

```
You: "Find the authenticate function"
Claude: *uses /cg-search authenticate --kind=function*
```

**From symptom**:
```
You: "Find functions related to user login"
Claude: *uses /cg-search login*
```

### Step 2: Confirm You Found the Right Function

```
You: "Show me the signature of authenticate"
Claude: *uses /cg-signature authenticate*
```

**Verify**: This matches the error stack trace or symptom.

---

## Phase 2: Trace Upstream (Find Who Called It) (3 minutes)

### Step 3: Find Direct Callers

```
You: "Who calls authenticate?"
Claude: *uses /cg-callers authenticate*
```

**Look for**:
- Is it called from multiple places?
- Which caller is likely triggering the bug?
- Any caller doing something unusual?

### Step 4: Trace the Call Chain

```
You: "Show me the full call chain to authenticate"
Claude: *uses /cg-callers authenticate --depth=3*
```

**Identify**:
- Entry point (HTTP handler, CLI command, etc.)
- Middleware or validation steps
- Where bad data might be introduced

### Step 5: Find the Source of Bad Data

For each caller in the chain:

```
You: "What does [callerName] do before calling authenticate?"
Claude: *uses /cg-callees [callerName]*
```

**Look for**:
- Data transformation steps
- Validation (or lack thereof)
- External input sources

---

## Phase 3: Trace Downstream (What Else Is Affected) (3 minutes)

### Step 6: See What the Buggy Function Calls

```
You: "What does authenticate call?"
Claude: *uses /cg-callees authenticate --depth=2*
```

**Identify**:
- Database queries
- External API calls
- Other functions that might fail

### Step 7: Find Similar Patterns

```
You: "What other functions call [problematicFunction]?"
Claude: *uses /cg-callers [problematicFunction]*
```

**Question**: Are other callers also affected by this bug?

---

## Phase 4: Root Cause Analysis (5 minutes)

### Step 8: Narrow Down the Problem

Ask targeted questions:

**Input validation**:
```
You: "Show me how input is validated before authenticate"
Claude: *traces validation functions*
```

**State management**:
```
You: "What functions modify user state before authenticate?"
Claude: *uses /cg-search setState or similar*
```

**Error handling**:
```
You: "How are errors handled in authenticate?"
Claude: *examines error handling code*
```

### Step 9: Find Similar Bugs

```
You: "Are there other functions with the same pattern?"
Claude: *uses /cg-search with patterns, checks implementations*
```

**Check**: Does this bug exist in multiple places?

---

## Example Scenarios

### Scenario 1: Null Pointer Exception

**Error**: `panic: runtime error: invalid memory address`

**Step 1**: Find where it crashes
```
Stack trace shows: processOrder -> calculateTotal -> getTaxRate
```

**Step 2**: Trace the call
```
You: "Show me the call chain to getTaxRate"
Claude: *uses /cg-callers getTaxRate --depth=3*

Result:
- handleCheckout calls processOrder
- processOrder calls calculateTotal
- calculateTotal calls getTaxRate
```

**Step 3**: Find the null source
```
You: "What does calculateTotal expect from processOrder?"
Claude: *uses /cg-signature calculateTotal*

Result: Expects *Order, but might be receiving nil
```

**Step 4**: Check validation
```
You: "How does handleCheckout validate the order?"
Claude: *uses /cg-callees handleCheckout*

Result: No validation before calling processOrder! Bug found.
```

**Fix**: Add validation in handleCheckout before calling processOrder.

---

### Scenario 2: Wrong Data in Database

**Symptom**: User sees wrong account balance

**Step 1**: Find balance update functions
```
You: "Find functions that update account balance"
Claude: *uses /cg-search updateBalance*
```

**Step 2**: Trace who calls them
```
You: "Who modifies the balance?"
Claude: *uses /cg-callers updateBalance*

Result:
- processPayment
- processRefund
- adjustBalance (admin function)
```

**Step 3**: Check all callers
```
You: "Show me how processRefund updates the balance"
Claude: *examines processRefund implementation*

Finding: processRefund adds instead of subtracts! Bug found.
```

**Fix**: Change addition to subtraction in processRefund.

---

### Scenario 3: Performance Issue

**Symptom**: Slow response on user profile page

**Step 1**: Find the handler
```
You: "Find the user profile handler"
Claude: *uses /cg-search handleUserProfile*
```

**Step 2**: Trace what it calls
```
You: "What does handleUserProfile call?"
Claude: *uses /cg-callees handleUserProfile --depth=2*

Result:
- getUserById
  - query database
- getUserPosts (calls database)
- getUserComments (calls database)
- getUserFriends (calls database)
- getUserNotifications (calls database)
```

**Finding**: N+1 query problem! Makes 5 separate DB calls.

**Fix**: Combine queries or use eager loading.

---

### Scenario 4: Security Vulnerability

**Report**: Unauthorized access to admin endpoints

**Step 1**: Find the endpoint
```
You: "Find admin endpoints"
Claude: *uses /cg-search admin --kind=function*
```

**Step 2**: Check authentication
```
You: "Show me what calls handleAdminPanel"
Claude: *uses /cg-callers handleAdminPanel*

Result: Called directly from HTTP router
```

**Step 3**: Trace middleware chain
```
You: "What middleware runs before handleAdminPanel?"
Claude: *examines router configuration*

Finding: No auth middleware! Bug found.
```

**Fix**: Add authentication middleware to admin routes.

---

## Debugging Patterns

### Pattern 1: The Null Chase

```
1. Find crash location
2. Trace upstream to data source
3. Find where null enters the system
4. Add validation at source
```

### Pattern 2: The State Inspector

```
1. Find where state is modified
2. Trace all callers that modify state
3. Identify conflicting modifications
4. Add synchronization or refactor
```

### Pattern 3: The Performance Profiler

```
1. Find slow function
2. Trace what it calls
3. Count database/network calls
4. Optimize or batch operations
```

### Pattern 4: The Security Auditor

```
1. Find sensitive functions
2. Trace who can call them
3. Verify authentication/authorization
4. Add missing checks
```

---

## Call Graph Red Flags

### 🚩 **Too Many Callers**
```
function X has 50+ callers
→ Might be doing too much
→ Changes are high-risk
→ Consider splitting
```

### 🚩 **Circular Dependencies**
```
A calls B, B calls C, C calls A
→ Tight coupling
→ Hard to test
→ Potential for infinite loops
```

### 🚩 **Deep Call Chains**
```
A → B → C → D → E → F (depth 6+)
→ Hard to understand flow
→ Many points of failure
→ Consider flattening
```

### 🚩 **Orphaned Functions**
```
function X has no callers
→ Dead code
→ Safe to remove
→ Or undiscovered entry point
```

### 🚩 **Duplicate Patterns**
```
Multiple functions with same dependencies
→ Possible code duplication
→ Extract common logic
```

---

## Tips

1. **Work backwards**: Start from error, trace to root cause
2. **Check both directions**: Use callers AND callees
3. **Use depth strategically**: Start with 1, increase if needed
4. **Look for patterns**: Similar bugs often have similar call graphs
5. **Document findings**: Note the call chain for bug report

---

## Debugging Checklist

When investigating a bug:

- [ ] Located the function where error occurs
- [ ] Traced upstream to find data source
- [ ] Checked all callers for similar patterns
- [ ] Traced downstream to find affected functions
- [ ] Identified root cause
- [ ] Verified fix won't break callers
- [ ] Checked for similar bugs elsewhere

---

## Integration with Other Tools

### Combine with Logging

```
1. Add logging at each level of call chain
2. Use /cg-callees to predict log sequence
3. Compare actual logs to expected sequence
4. Find where they diverge
```

### Combine with Debugger

```
1. Use /cg-callers to find entry point
2. Set breakpoint at entry
3. Use /cg-callees to predict execution flow
4. Step through and verify
```

### Combine with Tests

```
1. Use /cg-callees to understand dependencies
2. Mock dependencies in tests
3. Test each level of call chain separately
4. Use /cg-callers to ensure test coverage
```

---

## Advanced: Finding Transitive Bugs

**Problem**: Bug in X affects Y indirectly through Z

```
You: "What's the complete dependency tree of X?"
Claude: *uses /cg-callees X --depth=5*

You: "What depends on Z?"
Claude: *uses /cg-callers Z --depth=3*

Find: Y is in the caller tree of Z
Conclusion: Fixing X might affect Y
```

---

**Remember**: Call graphs reveal relationships. Use them to trace execution flow, find bug sources, and predict side effects of fixes.

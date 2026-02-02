# Workflow: Safe Refactoring with Impact Analysis

**Goal**: Safely modify or refactor code by understanding all dependencies and impacts.

**Time**: 10-20 minutes per function

**Prerequisites**:
- CodeGraph database is up to date
- Target function identified

---

## Phase 1: Understand Current State (3 minutes)

### Step 1: Get Function Signature

```
You: "Show me the signature of [functionName]"
Claude: *uses /cg-signature [functionName]*
```

**Document**:
- Current parameters and types
- Return type(s)
- Documentation/comments

### Step 2: Locate Definition

```
You: "Where is [functionName] defined?"
Claude: *uses /cg-search [functionName] --exact*
```

**Note**: File path and line number for reference.

---

## Phase 2: Impact Analysis (5 minutes)

### Step 3: Find All Callers

```
You: "Who calls [functionName]?"
Claude: *uses /cg-callers [functionName]*
```

**Analyze**:
- How many call sites?
- Spread across how many files?
- Are calls concentrated or widespread?

**Decision Point**:
- **Few callers (< 5)**: Safe to modify
- **Moderate (5-20)**: Plan carefully
- **Many (> 20)**: Consider deprecation strategy

### Step 4: Analyze Call Patterns

For each major caller:

```
You: "Show me how [callerName] uses [functionName]"
Claude: *reads the calling code*
```

**Look for**:
- Are parameters always the same?
- Any null checks or error handling?
- Can callers easily adapt to changes?

### Step 5: Check Downstream Dependencies

```
You: "What does [functionName] call?"
Claude: *uses /cg-callees [functionName] --depth=2*
```

**Assess**:
- Does it depend on external services?
- Are there database operations?
- Any side effects?

---

## Phase 3: Plan Refactoring (5 minutes)

### Step 6: Choose Refactoring Strategy

**Strategy A: Direct Modification** (< 5 callers)
```
1. Update function signature
2. Update all callers immediately
3. Run tests
```

**Strategy B: Deprecation** (5-20 callers)
```
1. Create new function with new signature
2. Mark old function as deprecated
3. Migrate callers incrementally
4. Remove old function after migration
```

**Strategy C: Adapter Pattern** (> 20 callers)
```
1. Create new function with new signature
2. Make old function call new function with adaptation
3. Migrate callers over time
4. Eventually remove old function
```

### Step 7: Identify Migration Order

Ask Claude to suggest:

```
You: "In what order should I update these callers?"
Claude: *analyzes call sites and suggests order*
```

**Consider**:
- Update most-called first (biggest impact)
- Or update least-called first (safest)
- Group by file/module

---

## Phase 4: Implementation (Variable)

### Step 8: Make Changes

**For Direct Modification**:
```
1. Modify function signature
2. Update implementation
3. Update all callers (use /cg-callers to track)
4. Run tests
```

**For Deprecation**:
```
1. Create [functionName]V2 or [newFunctionName]
2. Add deprecation notice to old function
3. Update one caller and test
4. Continue with remaining callers
```

### Step 9: Track Progress

```
You: "How many callers of [functionName] remain?"
Claude: *uses /cg-callers [functionName]*
```

After each batch of updates, run:
```bash
codegraph build
```

---

## Phase 5: Verification (2 minutes)

### Step 10: Verify No Callers Remain

After removing old function:

```
You: "Does anything still call [functionName]?"
Claude: *uses /cg-callers [functionName]*
```

**Expected**: No results (if you removed it) or only new calls to new version.

### Step 11: Check Integration

```
You: "Show me what now depends on [newFunctionName]"
Claude: *uses /cg-callers [newFunctionName]*
```

**Verify**: All expected callers migrated.

---

## Example: Adding Parameter

**Current**:
```go
func processPayment(amount float64, currency string) error
```

**New**:
```go
func processPayment(amount float64, currency string, metadata map[string]string) error
```

### Step-by-Step

**1. Analyze impact:**
```
You: "Show me all calls to processPayment"
Claude: *uses /cg-callers processPayment*
Result: 48 call sites across 4 files
```

**2. Choose strategy:**
```
Decision: Use Deprecation (many callers)
```

**3. Create new version:**
```go
// processPaymentV2 processes a payment with additional metadata
func processPaymentV2(amount float64, currency string, metadata map[string]string) error {
    // new implementation
}

// Deprecated: Use processPaymentV2 instead
func processPayment(amount float64, currency string) error {
    return processPaymentV2(amount, currency, make(map[string]string))
}
```

**4. Migrate incrementally:**
```
File 1: Update handleCheckout (25 call sites)
File 2: Update processSubscription (8 call sites)
File 3: Update handleRefund (3 call sites)
File 4: Update processRecurring (12 call sites)
```

**5. Remove old version:**
```go
// Delete processPayment once all callers migrated
```

---

## Example: Splitting Large Function

**Current**: 150-line function doing too much

**Strategy**: Extract into smaller functions

### Step-by-Step

**1. Understand dependencies:**
```
You: "What does processOrder call?"
Claude: *uses /cg-callees processOrder*
```

**2. Identify natural boundaries:**
```
Sections:
- Validation (calls: validateInput, checkInventory)
- Payment (calls: processPayment, recordTransaction)
- Fulfillment (calls: createShipment, sendNotification)
```

**3. Extract functions:**
```go
func processOrder(order Order) error {
    if err := validateOrder(order); err != nil {
        return err
    }
    if err := processOrderPayment(order); err != nil {
        return err
    }
    return fulfillOrder(order)
}

func validateOrder(order Order) error { /* ... */ }
func processOrderPayment(order Order) error { /* ... */ }
func fulfillOrder(order Order) error { /* ... */ }
```

**4. Verify callers still work:**
```
You: "What calls processOrder?"
Claude: *uses /cg-callers processOrder*
Result: All existing callers still work (signature unchanged)
```

---

## Safety Checklist

Before making changes:

- [ ] Current signature documented
- [ ] All callers identified
- [ ] Call patterns analyzed
- [ ] Dependencies mapped
- [ ] Refactoring strategy chosen
- [ ] Migration order planned
- [ ] Tests identified

After making changes:

- [ ] New signature documented
- [ ] All callers updated
- [ ] Tests passing
- [ ] No unexpected callers remain
- [ ] Code review completed
- [ ] Database rebuilt

---

## Common Scenarios

### Scenario: Changing Return Type

**Before**: `func getUser(id string) User`
**After**: `func getUser(id string) (User, error)`

**Impact**: Every caller must handle error

**Strategy**:
1. Find all callers: `/cg-callers getUser`
2. Add error handling to each
3. Update function signature

### Scenario: Renaming Function

**Before**: `func getUserById(id string) User`
**After**: `func GetUser(id string) User`

**Impact**: All callers must use new name

**Strategy**:
1. Create new function with new name
2. Make old function call new one
3. Mark old as deprecated
4. Migrate callers
5. Remove old function

### Scenario: Extracting Interface

**Before**: Direct calls to concrete type
**After**: Calls through interface

**Impact**: Can now mock/substitute implementations

**Strategy**:
1. Define interface
2. Update function signatures to use interface
3. Verify all callers pass compatible types

---

## Tips

1. **Rebuild frequently**: Run `/cg-build` after batches of changes
2. **Test incrementally**: Don't change everything at once
3. **Use version control**: Commit after each successful migration step
4. **Document decisions**: Note why you chose a particular strategy
5. **Ask for help**: Let Claude suggest migration strategies

---

## Troubleshooting

**"Can't find all callers"**:
```bash
# Rebuild database
codegraph build --force

# Verify
codegraph callers [functionName]
```

**"Too many callers to update manually"**:
```
Consider: Automated refactoring tools or scripting
Ask Claude: "How can I automate this migration?"
```

**"Circular dependencies"**:
```
You: "Show me the call chain"
Claude: *uses /cg-callees with depth to reveal cycle*
```

---

**Remember**: The goal is zero surprises. Use CodeGraph to know exactly what will change before changing it.

---
name: cg-signature
description: Get the complete signature of a function or method including parameters, return types, and documentation. Use when you need to understand how to call a function, what arguments it expects, or what it returns.
argument-hint: <function-name> [--lang=...]
allowed-tools: Bash(codegraph signature*)
---

# CodeGraph Function Signature

Get the complete signature and type information for a function or method.

## What This Does

For a given function, this skill shows:
- **Function name** and location
- **Complete signature** with parameter names and types
- **Return type(s)**
- **Documentation** (if available from LSP)
- **Scope** (package, class, module)

## Basic Usage

Get the signature of a function:
```bash
codegraph signature FunctionName
```

## Advanced Usage

**Filter by language:**
```bash
codegraph signature Config --lang=go
codegraph signature parse --lang=python,typescript
```

## Example Output

### Go Function
```
📋 Signature for NewManager:

  NewManager [function]
    internal/db/manager.go:20

    func NewManager(dbPath string) (*Manager, error)

    Parameters:
      - dbPath: string

    Returns:
      - *Manager
      - error

    Documentation:
      NewManager creates and initializes a new database manager.
      Returns an error if the database file cannot be opened.
```

### TypeScript Function
```
📋 Signature for validateUser:

  validateUser [function]
    src/auth/validator.ts:15

    function validateUser(user: User, options?: ValidationOptions): Promise<ValidationResult>

    Parameters:
      - user: User
      - options?: ValidationOptions (optional)

    Returns:
      - Promise<ValidationResult>
```

### Python Method
```
📋 Signature for process_request:

  process_request [method]
    app/handlers/request.py:42

    def process_request(self, request: Request, timeout: int = 30) -> Response

    Parameters:
      - self
      - request: Request
      - timeout: int = 30 (default: 30)

    Returns:
      - Response
```

## Use Cases

### Understanding APIs
Learn how to call a function you're unfamiliar with:
```bash
codegraph signature authenticate
```

### Documentation
Generate documentation for function signatures:
```bash
codegraph signature handleRequest
```

### Type Checking
Verify parameter types before calling:
```bash
codegraph signature parseJSON
```

### Refactoring
Before changing a function signature, see its current form:
```bash
codegraph signature processOrder
```

### Code Review
Quickly check function signatures during review:
```bash
codegraph signature validateInput
```

## Information Shown

The signature output includes:

1. **Function name and kind** (function, method, constructor)
2. **File location** (path and line number)
3. **Full signature** (exactly as written in code)
4. **Parameters breakdown**:
   - Parameter names
   - Types
   - Default values (if any)
   - Optional indicators
5. **Return types**:
   - Single return type
   - Multiple return values (Go, Python tuples)
   - Async/Promise types
6. **Documentation strings** (when available)
7. **Scope information** (class, package, module)

## Language-Specific Features

### Go
- Multiple return values shown separately
- Pointer types indicated
- Receiver types for methods

### TypeScript/JavaScript
- Optional parameters with `?`
- Union types
- Generic type parameters
- Promise return types

### Python
- Type hints (when present)
- Default argument values
- `self` parameter for methods
- Decorator information

### Java
- Access modifiers (public, private, protected)
- Static/instance distinction
- Throws declarations
- Generic type bounds

### Rust
- Lifetime parameters
- Mutable references
- Trait bounds
- Generic constraints

## Flags Reference

| Flag | Description | Example |
|------|-------------|---------|
| `--lang` | Filter by language(s) | `--lang=go,python` |

## Comparison with Related Skills

| Skill | Purpose | Output |
|-------|---------|--------|
| `/cg-signature` | Get function signature | Parameters, return types, docs |
| `/cg-search` | Find where defined | File location, symbol kind |
| `/cg-callers` | Find who calls it | Call sites, callers list |
| `/cg-implementations` | Find implementations | Implementing classes |

## When to Use

Use this skill when you need to:
- **Call a function**: "What parameters does this expect?"
- **Understand types**: "What type does this return?"
- **Read documentation**: "What does this function do?"
- **Verify changes**: "What's the current signature?"
- **Generate docs**: "Document this API"

## Important Notes

- **Must be indexed**: Run `/cg-init` first to build the database
- **Rebuild after changes**: Run `/cg-build` to update signatures
- **LSP-based**: Uses language server data for accurate types
- **Documentation**: Shows LSP hover information when available

## Example Workflow

**1. Find a function:**
```bash
codegraph search validate --kind=function
```

**2. Get its signature:**
```bash
codegraph signature validateRequest
```

**3. Find usage examples:**
```bash
codegraph callers validateRequest
```

**4. See what it depends on:**
```bash
codegraph callees validateRequest
```

## Troubleshooting

**Signature not found:**
- Verify function exists: `/cg-search FunctionName`
- Rebuild database: `/cg-build --force`
- Check spelling of function name

**Incomplete type information:**
- Some languages/LSPs provide limited type info
- Check source code directly for full details
- TypeScript/Go have best type information

**No documentation shown:**
- Not all functions have doc comments
- LSP may not extract documentation for all languages
- Check source file for comments

**Multiple matches:**
- Use `--lang` to filter by language
- Use full qualified name (e.g., `pkg.FunctionName`)
- Check output for file paths to identify correct match

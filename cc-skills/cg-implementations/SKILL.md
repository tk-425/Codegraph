---
name: cg-implementations
description: Find all implementations of an interface, abstract class, or protocol. Use when exploring polymorphism, finding concrete types, or understanding which classes implement a given contract.
argument-hint: <interface-name> [--lang=...]
allowed-tools: Bash(codegraph implementations*)
---

# CodeGraph Interface Implementations

Find all concrete types that implement a given interface, abstract class, or protocol.

## What This Does

For a given interface, this skill shows:
- **All implementing types** (classes, structs)
- **Where defined** (file and line number)
- **Implementation details** (method count, language)

Works with:
- **Go**: interfaces
- **TypeScript/JavaScript**: interfaces and abstract classes
- **Java**: interfaces and abstract classes
- **Rust**: traits
- **Python**: protocols and abstract base classes

## Basic Usage

Find implementations of an interface:
```bash
codegraph implementations InterfaceName
```

## Advanced Usage

**Filter by language:**
```bash
codegraph implementations Reader --lang=go
codegraph implementations Service --lang=typescript,java
```

## Example Output

### Go Interface
```
🔍 Implementations of Reader (4 found):

  FileReader [struct]
    internal/io/file.go:15
    Implements: Read(p []byte) (n int, err error)

  BufferedReader [struct]
    internal/io/buffered.go:28
    Implements: Read(p []byte) (n int, err error)

  StringReader [struct]
    internal/io/string.go:42
    Implements: Read(p []byte) (n int, err error)

  NetworkReader [struct]
    internal/io/network.go:65
    Implements: Read(p []byte) (n int, err error)
```

### TypeScript Interface
```
🔍 Implementations of Service (3 found):

  UserService [class]
    src/services/user.ts:12
    Implements: init(), process(), cleanup()

  AuthService [class]
    src/services/auth.ts:25
    Implements: init(), process(), cleanup()

  DataService [class]
    src/services/data.ts:38
    Implements: init(), process(), cleanup()
```

### Java Interface
```
🔍 Implementations of Vehicle (3 found):

  Car [class]
    src/vehicles/Car.java:10
    Implements: start(), stop(), getSpeed()

  Motorcycle [class]
    src/vehicles/Motorcycle.java:45
    Implements: start(), stop(), getSpeed()

  Truck [class]
    src/vehicles/Truck.java:78
    Implements: start(), stop(), getSpeed()
```

## Use Cases

### Discovering Implementations
Find all concrete types that fulfill a contract:
```bash
codegraph implementations Handler
```

### Polymorphism Analysis
See what types can be used interchangeably:
```bash
codegraph implementations Repository
```

### Design Pattern Exploration
Understand how patterns are implemented:
```bash
codegraph implementations Strategy
codegraph implementations Observer
```

### API Understanding
Learn what concrete types exist for an abstraction:
```bash
codegraph implementations Database
```

### Refactoring
Before modifying an interface, see all affected implementations:
```bash
codegraph implementations Service --lang=go
```

## Design Pattern Examples

### Strategy Pattern
```bash
codegraph implementations PaymentStrategy
# Returns: CreditCardPayment, PayPalPayment, CryptoPayment
```

### Factory Pattern
```bash
codegraph implementations Product
# Returns: ConcreteProductA, ConcreteProductB
```

### Repository Pattern
```bash
codegraph implementations Repository
# Returns: UserRepository, OrderRepository, ProductRepository
```

### Observer Pattern
```bash
codegraph implementations Observer
# Returns: LogObserver, EmailObserver, MetricsObserver
```

## Language-Specific Behavior

### Go
- Finds all types that implement interface methods
- Implicit implementation (no `implements` keyword)
- Shows method signatures

### TypeScript/JavaScript
- Finds classes with `implements InterfaceName`
- Finds classes extending abstract classes
- Shows implemented methods

### Java
- Finds classes with `implements InterfaceName`
- Finds classes with `extends AbstractClass`
- Shows full class hierarchy

### Rust
- Finds types with `impl TraitName`
- Shows trait bounds
- Generic implementations

### Python
- Finds classes inheriting from `Protocol`
- Finds classes extending `ABC` (Abstract Base Classes)
- Duck-typed implementations may be incomplete

## Flags Reference

| Flag | Description | Example |
|------|-------------|---------|
| `--lang` | Filter by language(s) | `--lang=go,typescript` |

## Comparison with Related Skills

| Skill | Purpose | Output |
|-------|---------|--------|
| `/cg-implementations` | Find implementing types | List of concrete classes |
| `/cg-search` | Find interface definition | Where interface is defined |
| `/cg-callers` | Find where interface is used | Call sites using interface |

## When to Use

Use this skill when you need to:
- **Explore abstractions**: "What types implement this interface?"
- **Understand polymorphism**: "What can I pass to this function?"
- **Find concrete types**: "What are the actual implementations?"
- **Plan changes**: "What will break if I modify this interface?"
- **Learn codebase**: "How is this pattern implemented?"

## Example Analysis Workflow

**1. Find an interface:**
```bash
codegraph search Repository --kind=interface
```

**2. Find all implementations:**
```bash
codegraph implementations Repository
```

**3. Check a specific implementation's signature:**
```bash
codegraph signature UserRepository
```

**4. See where implementations are used:**
```bash
codegraph callers UserRepository
```

## Common Patterns

### Dependency Injection
```bash
codegraph implementations Service
# See all injectable services
```

### Plugin Architecture
```bash
codegraph implementations Plugin
# Find all available plugins
```

### Data Access Layer
```bash
codegraph implementations DataSource
# See database, API, cache implementations
```

### Event Handlers
```bash
codegraph implementations EventHandler
# Find all event processors
```

## Important Notes

- **Must be indexed**: Run `/cg-init` first to build the database
- **Rebuild after changes**: Run `/cg-build` to update implementations
- **LSP-based**: Uses language server protocol for accurate results
- **Explicit implementations**: Works best with explicit `implements`/`extends`
- **Language support varies**: Best support in Go, TypeScript, Java

## Troubleshooting

**No implementations found:**
- Interface might not be implemented yet
- Try searching for the interface: `/cg-search InterfaceName`
- Rebuild database: `/cg-build --force`
- Check if language is supported for implementation detection

**Incomplete results:**
- Some languages use implicit implementation (hard to detect)
- Duck-typed languages (Python) may miss implementations
- Rebuild database: `/cg-build --force`

**Wrong interface name:**
- Use `/cg-search` to find the exact interface name
- Check spelling and capitalization
- Try language filter if name is ambiguous

**Too many results:**
- Use `--lang` to narrow by language
- Check if interface name is too generic
- Consider if this indicates design issue (too broad interface)

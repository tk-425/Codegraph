# Reliability characterization fixtures

Source specification: `.docs/2026-11-08-212127-fix-spec.md`, S0.

These tracked fixtures do not depend on the ignored `tests/` directory or an installed language server. The Go files under `refresh/` are alternative states of one source file; they are not intended to compile together as a package.

## Running

From the repository root, using already-cached Go dependencies and a local Go toolchain with CGO:

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=1
# Passing fixture/transport/process controls:
go test -tags characterization -run '^TestCharacterizationControl' -count=1 -timeout=120s ./...
# Desired-behavior assertions; known defects are expected to fail during S0:
go test -tags characterization -count=1 -timeout=120s \
  ./internal/indexer ./internal/lsp ./internal/search ./cmd/codegraph
```

The second command is not a passing acceptance suite yet. Each `F01`–`F08` test name identifies its finding; messages cite the relevant `T` acceptance scenario. A future fix must make the corresponding assertions pass, then promote that coverage into the ordinary suite when it no longer needs opt-in. Never invert an assertion merely to preserve today's broken behavior.

## Coverage boundaries

- `refresh/`: rename a declaration, change bytes without advancing mtime, ignore the last file, repeat fallback, and fail a database write.
- `identity/`: same Go method name on different receivers and a Python class method invoking a function.
- `calls/` and `lsp/references.json`: a function value reference, not an invocation.
- `hierarchy/` and `lsp/hierarchy.json`: a local semantic parent identified by URI/range, not by its database ID.
- `lsp/main.go`: a scripted protocol peer with responsive, exit-on-request, blocked-input, and ignored-exit modes. Fixtures can specialize methods by source line (`method@line`) or hierarchy item name (`method@name`).
- Search checks syntax provenance surviving storage; it does not claim build-quality metadata already exists.
- CLI checks re-execute the Go test binary into the actual `main()` entry point, isolating cwd/HOME, stdout, stderr and exit status. They are not command-buffer-only checks.

The scripted peer exercises real OS processes, framed stdin/stdout and the existing LSP client. It does **not** prove compatibility or semantic accuracy with gopls, pyright, or another real server. Real-server integration remains a separate, explicitly unverified environment-dependent gate.

## Isolation and termination

Fixture copies, SQLite databases and compiled peers live in `t.TempDir()`. Missing-provider paths are absolute paths inside those workspaces, never executable names resolved accidentally from the user's PATH. Tests use caller deadlines and outer watchdogs; blocked-request stacks are captured before recovery. A peer still alive after client shutdown is killed by the harness, and the original waiter is allowed to reap it rather than starting a second Wait. Passing cleanup is not evidence that production cleanup is fixed.

No installed `codegraph`, user registry, project index or real account is modified. Normal Go caches may be populated. The test harness owns its temporary files and processes; no persistent scratch files are required.

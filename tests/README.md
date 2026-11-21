# Tests

This tree follows the split used by `godot-mcp` between fast domain/API tests and integration fixtures, while retaining Go's conventional colocated unit tests for private helpers.

- `core/`: external-contract tests exercising only exported domain APIs and optimistic graph transactions.
- `mcp/`: executable-level JSON-RPC/stdio tests, not just direct calls into MCP handlers.
- `integration/`: opt-in GPU/Material Maker smoke tests; skipped unless explicitly configured.
- `fixtures/`: minimal purpose-built `.mmg` and `.ptex` examples, not a preset library or cookbook.

Run `make test` for the hermetic suite, `make test-race` for race checks, and `make check` for formatting, vet and pinned static analyzers. GPU smoke requires `MM_SMOKE_GODOT`, `MM_SMOKE_PROJECT` and `MM_SMOKE_PTEX` paths and executes the trusted local engine only when supplied.

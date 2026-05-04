# AGENTS.md

Guidance for AI agents (Claude Code, Codex, etc.) working in this repository.

This is **Soft Serve**, a self-hostable Git server written in Go (`module github.com/charmbracelet/soft-serve`, Go 1.25+). The codebase already organizes domain logic behind interfaces (`pkg/store`, `pkg/storage`, `pkg/backend`); new external integrations must follow the same discipline, with stricter rules described below.

---

## Non-negotiable rules

1. **Strict TDD.** No production code is written before a failing test exists for it.
2. **Ports + adapters for every external service.** Anything that crosses a process boundary (network, disk, OS, third-party API, clock, randomness, env) is reached through a port (interface) defined by the domain, with one or more adapters implementing it.
3. **No mocks of types you don't own.** Wrap third-party SDKs behind a port first; mock the port, never the SDK.
4. **Keep the dependency arrow pointing inward.** Domain/business code may not import adapter packages. Adapters depend on domain interfaces, not the other way around.

If a request seems to require breaking one of these, stop and surface the conflict to the user before proceeding.

---

## Strict TDD workflow

Follow red → green → refactor on every change that touches behavior.

1. **Red.** Write the smallest failing test that expresses the next required behavior. Run it (`go test ./path/...`) and confirm it fails for the *right* reason (compile error counts as red only when the missing symbol is the thing under test).
2. **Green.** Write the minimum production code that makes the test pass. Resist generalizing.
3. **Refactor.** With tests green, clean up names, duplication, and structure. Re-run tests after each change.
4. Commit only with all tests green. Never commit a skipped or `t.Skip`-disabled test as the "fix".

Operational rules:

- One behavior per test. Prefer table-driven tests (`tests := []struct{...}`) when the behavior space is enumerable; one `t.Run` per case.
- Test names describe behavior, not implementation: `TestRepo_Create_RejectsDuplicateName`, not `TestCreateFunc`.
- Use `testing.T.Helper()` in test helpers. Use `t.Cleanup` over `defer` for resources owned by the test framework.
- Prefer `testify/require` for fatal assertions and `testify/assert` for non-fatal — both are already in `go.mod`.
- No network, no real filesystem outside `t.TempDir()`, no real clock in unit tests. All such dependencies go through ports (see below).
- Integration tests that need Postgres or a real Git binary live alongside the unit tests but are gated by build tags or env (`SOFT_SERVE_DB_DRIVER=postgres`, see `.github/workflows/build.yml`). Mark them clearly and keep the unit suite hermetic.
- A bug fix starts with a regression test that fails on `main` and passes after the fix. No exceptions.

When asked to "just add" something without a test, push back: write the test first, or explain why the change is genuinely test-exempt (e.g. pure rename, comment edit, dependency bump).

---

## Ports + adapters for external services

A new external service is anything the process talks to that isn't pure Go in this module: HTTP/gRPC APIs, databases, object storage, message queues, mail/SMS, OAuth providers, Git remotes outside the local FS, the system clock, `os.Getenv`, `crypto/rand`, etc.

### Layout

```
pkg/<domain>/                 # domain types + port interfaces, no I/O
pkg/<domain>/<domain>_test.go # tests against fakes implementing the ports
pkg/<domain>/adapters/<svc>/  # one adapter per concrete backend (e.g. s3, postgres, http)
pkg/<domain>/adapters/fake/   # in-memory fake used by tests across packages
```

Existing precedents to mirror:

- `pkg/storage/storage.go` defines the `Storage` port; `pkg/storage/local.go` is the local-FS adapter. New blob backends (S3, GCS, etc.) go in sibling files/packages, not inside `local.go`.
- `pkg/store/store.go` composes per-aggregate ports; `pkg/store/database/` holds the SQL adapter. New persistence backends are new adapter packages, not edits to `database/`.
- `pkg/backend` is the application layer that depends on the ports, not on adapters.

### Rules for adding a new external integration

1. **Define the port first, in the domain package.** It expresses what the *domain* needs in domain terms, not what the SDK offers. Methods take and return domain types or stdlib types, never vendor types.
2. **Write the failing test against a fake adapter.** The fake lives in `adapters/fake/` and is the reference implementation used to lock in the port's contract. If the port is hard to fake, the port is wrong — redesign before continuing.
3. **Implement the real adapter.** It imports the SDK; the domain package must not. Keep adapter code thin: translate domain calls to SDK calls and SDK errors to domain errors. No business logic in adapters.
4. **Wire the adapter at the composition root** (`cmd/soft` and the relevant constructor in `pkg/backend` or `pkg/config`). Selection between adapters is config-driven, the same way `SOFT_SERVE_DB_DRIVER` already selects Postgres vs SQLite.
5. **Contract tests.** When more than one adapter implements a port, write a shared contract-test suite (e.g. `func RunStorageContract(t *testing.T, s Storage)`) and run it against every adapter, including the fake. This is how the fake stays honest.
6. **Errors are part of the port.** Define sentinel errors (or typed errors) in the domain package; adapters must translate to them. Callers never `errors.Is` against an SDK-specific error.
7. **Context everywhere.** Every port method that may block takes `context.Context` as its first argument.
8. **No global state in adapters.** Construct adapters with explicit config; pass them in. No `init()` registration, no package-level singletons.

### What this rules out

- Calling `http.Get`, `os.Open`, `time.Now`, `os.Getenv`, `sql.Open`, an SDK client, etc. directly from `pkg/backend`, handlers, UI code, or any domain package.
- "Temporary" direct calls "until we have time to refactor". Add the port now or don't add the feature.
- Mocking `*sql.DB`, `*http.Client`, an AWS SDK type, etc. The mock target is always our own interface.

### Clock and randomness

Treat these as external services too. If you need `time.Now`, inject a `Clock` interface (`Now() time.Time`); if you need randomness, inject a source. Tests use deterministic fakes.

---

## Project conventions worth knowing

- Build/test: `go build ./...`, `go test ./...`. Postgres-backed tests need `SOFT_SERVE_DB_DRIVER=postgres` and `SOFT_SERVE_DB_DATA_SOURCE=...` (see `.github/workflows/build.yml`).
- End-to-end tests live under `testscript/` using `rogpeppe/go-internal/testscript`. New external integrations should grow a testscript scenario alongside the unit/contract tests where user-visible behavior changes.
- Logging uses `charm.land/log/v2`. Pass loggers through context (`pkg/log`); don't reach for `log.Default()`.
- Errors: prefer wrapping with `fmt.Errorf("...: %w", err)`. Domain packages export sentinel errors for callers to match on.
- Keep changes scoped. Don't refactor unrelated code in the same change; if you spot something, mention it instead of silently editing.

---

## When the user asks for something that violates these rules

- Asked to skip tests? Write the test first; if they push back, ask why and record the answer.
- Asked to call an SDK directly from domain code? Propose the port + adapter split in one short paragraph and wait for confirmation before writing code.
- Asked to mock a third-party type? Wrap it first, then mock the wrapper.

These rules exist because new external services are arriving soon and the cost of getting the seam wrong compounds quickly. Treat them as load-bearing.

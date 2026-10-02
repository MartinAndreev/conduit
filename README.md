# Conduit — Go rewrite A

Fresh Go scaffold on `go-rewrite-a`, based on TypeScript baseline
`5d6615e8319b0d069503a38e14ae86f73fd3d1ea` (Conduit v0.7.0).
Git history is preserved. The old application, website, specifications, build
pipelines and runtime dependencies have been removed from this checkout.

This is not a working replacement for Conduit yet. The CLI exposes help/version
bootstrap behavior. Internal Codex/OpenCode harness adapters and local Turso
persistence are implemented and tested, but are not wired into a runnable
application workflow. Application orchestration and UI remain unimplemented.

## Development

Use Go 1.26 or newer with a current patch release.
See the [official downloads](https://go.dev/dl/).

```sh
go run ./cmd/conduit --help
go run ./cmd/conduit --version
go build -o bin/conduit ./cmd/conduit
go vet ./...
go test ./...
```

The module keeps the existing repository identity:
`github.com/MartinAndreev/conduit`. Introduce internal packages only when an
approved implementation slice needs them.

The consumer-owned [harness provider contract](internal/sessions/provider/README.md)
defines typed session, control, capability, event, and error boundaries. Concrete
[harness adapters](specs/001-harness-storage/harnesses.md) implement verified
protocol slices with explicit unsupported capabilities. The
[storage adapter](specs/001-harness-storage/storage.md) uses local Turso and
Jet-generated table types; its initial schema bootstrap is not a complete
migration runner, legacy importer, or remote database integration.

The [Steer DDD pilot](specs/002-steer-ddd/spec.md) separates correction rules from
adapter procedures, with focused effect steps and shared mutex scopes. The
[walkthrough](specs/002-steer-ddd/walkthrough.md) explains the code and review
hotspots. Start with [AGENTS.md](AGENTS.md) for task-specific agent instructions
and project skills.

Tests use fake harness transports and temporary local databases; passing them
does not establish behavior of every live model/harness version. On the current
development host, ordinary storage-test linking is blocked by a macOS SDK/linker
mismatch. `CGO_ENABLED=0 go test ./...` exercises the full suite, including local
Turso operations, without that link path. This is an environment workaround,
not a change to the system toolchain.

## Commit-message lint

The original Conventional Commits configuration remains. Allowed types are
`feat`, `fix`, `refactor`, `test`, `docs`, and `chore`.

```sh
pnpm install --frozen-lockfile
git config core.hooksPath .husky
```

The commit hook runs commitlint directly; Husky itself is not required.
Node and pnpm are development dependencies for this retained tooling only;
the Go executable does not require them. The previous TypeScript pre-commit,
pre-push and lifecycle hooks have been removed.

## Planning

The initial migration guide is in the sibling directory
[`conduit-re-write-specs`](../conduit-re-write-specs/README.md).
It currently lives outside this Git repository; commits here do not version it.
The original TypeScript checkout remains the read-only investigation reference.

The rewrite will improve screens, workflow, orchestration and agent feedback.
Separate observed legacy behavior, invariants to preserve and proposed changes.
Do not begin product implementation until the relevant task group is assigned.

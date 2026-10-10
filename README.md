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

## Collaboration

### Branch naming

Use one of the following prefixes for new branches, followed by a short lowercase kebab-case description:

| Prefix | Purpose | Example |
| --- | --- | --- |
| `feature/` | Add new application functionality. | `feature/application-startup` |
| `fix/` | Correct a defect in existing functionality. | `fix/startup-config-validation` |
| `hotfix/` | Deliver an urgent correction to a released version. | `hotfix/config-data-loss` |
| `refactor/` | Restructure code while preserving intended behavior. | `refactor/startup-procedures` |
| `docs/` | Change documentation, specifications, or design references. | `docs/startup-contract` |
| `test/` | Add or improve tests without changing application behavior. | `test/concurrent-initialization` |
| `chore/` | Maintain dependencies, tooling, or repository configuration. | `chore/update-go-toolchain` |
| `ci/` | Change continuous integration or automated delivery workflows. | `ci/windows-startup-checks` |
| `perf/` | Improve runtime performance or resource consumption. | `perf/harness-discovery` |
| `build/` | Change compilation, bundling, or packaging. | `build/desktop-assets` |
| `release/` | Prepare and stabilize a specific release. | `release/0.2.0` |

Choose the prefix matching the branch's primary purpose. Supporting tests and documentation for a feature belong on that feature branch. Do not invent another prefix without agreement.

Do not use `codex/` as the default prefix. Follow any exact branch name explicitly requested by the user. This convention applies to future branches; it does not implicitly rename existing branches.

Branch prefixes and commit-message types are separate conventions. Commit messages
must still use the types allowed by the commit-message lint configuration below.

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

# Collaboration and delegation

- The engineer working on the feature owns product and architectural decisions; the assistant collaborates on architecture, presenting proposals and tradeoffs, and obtains agreement before architectural implementation.
- Distinguish accepted decisions from suggestions and open questions. Ask about missing material details; continue independent authorized work while awaiting answers. Do not repeat settled questions.
- When coordinating implementation or tests through agents, use a personal orchestration skill (such as `agent-orchestration`) if available for roles, assignment structure, and delegation rules.
- Each assignment names the approved outcome and contract, owned files/packages, dependencies, required behavior and error semantics, acceptance checks, and excluded scope. Require unresolved decisions to be reported.
- Coordinate ownership of shared files. Parallelize independent tasks only. Availability of an agent does not authorize later task groups.
- After implementing a feature, offer to run the repository's `mentoring` skill to walk through what was built and explain the Go-specific details in the conversation language; do not load it automatically.
- Exact domain boundaries, package layout, and unapproved library/framework choices must be discussed rather than inferred from older TypeScript conventions.

## Branch naming

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

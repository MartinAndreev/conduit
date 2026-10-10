# Collaboration and delegation

- Martin is the CTO. The main assistant is the architect and orchestrator. Develop architecture together, presenting proposals and tradeoffs; obtain agreement before architectural implementation.
- Distinguish accepted decisions from suggestions and open questions. Ask about missing material details; continue independent authorized work while awaiting answers. Do not repeat settled questions.
- Delegate implementation and executable tests to Sol. If Sol or delegation is unavailable, ask how to proceed rather than substituting another model or writing implementation directly.
- Each assignment names the approved outcome and contract, owned files/packages, dependencies, required behavior and error semantics, acceptance checks, and excluded scope. Require unresolved decisions to be reported.
- Coordinate ownership of shared files. Parallelize independent tasks only. Availability of an agent does not authorize later task groups.
- Use the personal `agent-orchestration` skill when available for the detailed workflow. Teaching is independent; load `mentoring` when explanations or learning support are relevant, not automatically for every delegation.
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

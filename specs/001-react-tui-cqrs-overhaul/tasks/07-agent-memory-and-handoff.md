# Group 7 — Agent memory and refinement handoff

## Outcome

Give every role a small, evidence-backed, feature-specific handoff so it can make focused changes without scanning the repository. Memory remains local, bounded, refreshable, and optional. The architect is used only for material decision curation; deterministic indexing and cheap summarization handle normal refreshes.

This group starts only after Group 6 provides project-local SQLite repositories and migration infrastructure.

## User workflow

The refinement preview gains a **Handoff preparation** panel before approval. It contains these persistent, per-revision toggles:

| Toggle                  | Default | Action on approval                                                                                                                        |
| ----------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `indexRepository`       | on      | Deterministically index changed repository facts, ownership, entry points, exports/imports, tests, and package boundaries. No model call. |
| `refreshFeatureMemory`  | on      | Use the configured cheap summarizer to merge new feature/run evidence into bounded memory observations.                                   |
| `includeRoleHandoffs`   | on      | Generate a role-specific `handoff.md` before each worker run and attach it to that role’s prompt.                                         |
| `architectCurateMemory` | off     | Ask the architect to resolve conflicting or ambiguous observations after packet refinement. This is explicitly cost-bearing.              |

- The preview shows an estimated action cost: `free`, `cheap model`, or `architect model`.
- Toggle state is saved before architect execution in `specs/<feature>/revisions/<revision>/memory-handoff.md`, so the packet records what was prepared and why. It contains no secrets, raw transcript, or generated embeddings.
- On `a` approval, selected handoff actions run in order: index → cheap refresh → optional architect curation → architect refinement/worker handoff. Individual action failure is displayed with retry/skip; it never silently changes a toggle.
- `r` returns to the refinement form without running handoff actions. `q` exits without running them.
- Existing CLI refinement remains scriptable: `conduit refine <id> --index-memory`, `--refresh-memory`, `--include-handoff`, and `--curate-memory`; explicit `--no-*` flags override defaults. Compact output reports action status and token budget only, never prompt contents.

## Memory model

Memory is not a raw transcript store and does not automatically inject arbitrary past agent output into prompts.

### Tier 1 — deterministic repository index

`conduit memory index` produces project-local index records without an LLM:

- package/workspace names and entry points;
- directory/role ownership from `conduit.yml`;
- source/test file relationships and exported symbols/import edges when available from TypeScript metadata or conservative source parsing;
- canonical docs (`AGENTS.md`, architecture, coding standards) by path and content hash;
- git HEAD and per-file content hashes.

Do not parse or index `.env`, credentials, `.conduit` secrets, binary assets, `node_modules`, build outputs, or ignored/private paths. Index failures produce evidence-backed warnings, not invented facts.

### Tier 2 — feature observations

The memory store uses append-only observations:

```ts
interface MemoryObservation {
  id: string;
  projectId: string;
  featureId?: string;
  runId?: string;
  kind:
    | "decision"
    | "contract"
    | "implementation"
    | "test"
    | "risk"
    | "repository_fact";
  summary: string;
  evidence: readonly MemoryEvidence[];
  confidence: "approved" | "verified" | "unverified" | "stale";
  source:
    | "human"
    | "packet"
    | "run"
    | "review"
    | "index"
    | "summarizer"
    | "architect";
  createdAt: string;
  invalidatedAt?: string;
  tokenCount: number;
}

interface MemoryEvidence {
  path: string;
  gitSha?: string;
  lineStart?: number;
  lineEnd?: number;
  artifactId?: string;
}
```

- Every observation must have at least one local evidence path or be explicitly labelled `unverified` with the originating run/revision ID.
- Packet decisions and user clarification answers are imported as `approved`; successful test results/diffs are `verified`; model output starts `unverified`.
- Any index refresh marks observations citing a changed file/hash as `stale`; stale observations never enter a default role handoff.
- The store is project scoped. A project cannot retrieve another project’s observations.

### Tier 3 — bounded role handoff

`buildRoleMemoryHandoff(featureId, role)` deterministically composes:

1. the role’s skill, ownership, and assigned task/contract files;
2. approved decisions and non-stale verified observations for the feature;
3. relevant repository facts for owned paths and direct imports only;
4. changed files and latest test/review evidence for the selected run;
5. unresolved questions and risks.

The generated handoff has hard budgets:

- 1,200 tokens default, 1,800 maximum;
- at most 12 observations and 20 cited paths;
- each observation summary at most 160 tokens;
- packet/contract citations take precedence over model-generated observations;
- preserve IDs so a role can request detail by ID rather than receiving a larger prompt.

The role prompt instructs: read the handoff first; do not scan unrelated directories; use `conduit memory search` for a precise question; read only cited files or results returned by the search. Memory is helpful context, not authority over the approved packet or current source.

## Commands, queries, and persistence

Create an owning `src/domains/memory/` domain with individual command/query contract files, handlers, types, errors, and repository interfaces. Do not put memory types in runners, refinement, system, or TUI modules.

Required commands:

- `indexRepositoryMemory`
- `refreshFeatureMemory`
- `curateFeatureMemory`
- `buildRoleMemoryHandoff`
- `invalidateMemoryForFiles`
- `setRefinementHandoffOptions`

Required queries:

- `searchMemory`
- `getRoleMemoryHandoff`
- `getMemoryStatus`
- `getRefinementHandoffOptions`

Required interfaces:

- `MemoryRepository` — SQLite-backed append/read/invalidate/compact operations;
- `RepositoryIndexer` — deterministic, read-only repository discovery;
- `MemorySummarizer` — cheap-model adapter returning constrained JSON observations;
- `MemoryCurator` — architect-model adapter used only by the explicit curate action;
- `MemoryRedactor` — shared secret/path filtering before persistence or prompt construction.

Group 6’s `.conduit/state.db` stores observations, evidence references, index snapshots, compaction records, handoff cache keys, and action status. Raw runner transcripts stay in existing local artifacts; they are referenced by ID, not copied into memory by default. Use WAL and transactional append/invalidate operations.

## Refresh, compaction, and cost policy

- Index incrementally by git SHA and file hash; unchanged files are never re-indexed.
- Refresh only observations affected by changed files, a new packet revision, new test result, review finding, or explicit user request.
- Cheap summarization receives only the previous compact observation set plus changed evidence, never the complete repository or transcript.
- Trigger deterministic compaction when a feature exceeds 60 active observations or 8,000 stored summary tokens. The cheap summarizer creates a replacement summary with evidence IDs; old records remain append-only but are excluded from default retrieval.
- Architect curation runs only when the user enables it, when approved decisions conflict, or when compaction reports unresolved contradictory evidence. It receives the compact conflict set, not every observation.
- Record token input/output estimates and model identity in local action metadata. Do not record prompts, secrets, or raw completions.

## Security and trust

- Memory must pass the same secret-redaction policy as logs, prompts, and SQLite state.
- Treat model-produced summaries as untrusted context. They cannot override user answers, approved packet content, contracts, source code, or tests.
- Never automatically inject an observation containing executable instructions, credentials, external URLs, or unverified security claims.
- Provide `conduit memory forget <id>` and `conduit memory purge --feature <id>`; purge deletes local memory/index/cache rows but not committed packet files.

## Tests and acceptance criteria

- Toggling actions in refinement preview persists and reloads `memory-handoff.md`; reject/quit performs no action.
- Indexing makes zero model calls and skips excluded/sensitive paths.
- Refresh sends only changed evidence and respects configured token/observation budgets.
- Stale observations are excluded after a cited file changes.
- Role handoff includes ownership, approved decisions, citations, and no more than the configured budget.
- Search returns project-scoped, cited observations and never stale/untrusted observations unless explicitly requested.
- Architect curation is never invoked unless enabled or a documented conflict trigger occurs.
- No secret, raw transcript, prompt, or cross-project observation is persisted or exposed.
- Compact CLI flags and TUI flows remain backward compatible.

Run `pnpm format`, `pnpm typecheck`, `pnpm lint`, `pnpm test`, `pnpm build`, `pnpm build:standalone linux-x64`, and `pnpm start --help`.

## Non-goals

- Vector databases, embeddings, Chroma, or external memory services in the initial release.
- Automatic architect curation on every worker run.
- Replacing committed specs/contracts with generated memory.
- Cross-project or cloud-synchronized memory.

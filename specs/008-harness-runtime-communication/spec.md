# Feature 008: Harness Runtime Communication

## Status

Proposed. This specification defines the next harness integration boundary. It does not amend the approved `AgentAssignmentV1`, `AgentResponseV1`, or `ConduitResultRecordV1` contracts from Feature 007 until corresponding versioned contracts and implementation tasks are approved.

## Problem

Conduit supports `codex`, `opencode`, `pi`, and `kilo` through runner adapters, but the current launch and parsing boundaries are inconsistent:

- Codex is launched with `exec --json` and supports adapter-owned final-message capture.
- OpenCode and Kilo have JSON parsers, but Conduit launches `run` without enabling their documented JSON output mode.
- Pi has a JSON parser, but Conduit launches print mode rather than Pi JSON or RPC mode.
- The orchestrator always ignores child stdin, so it cannot drive a bidirectional ACP or RPC session.
- Adapter availability is executable-only; Conduit does not negotiate a harness version or capabilities.
- Current runner events collapse reasoning summaries, commands, reads, permissions, usage, and arbitrary stdout into a small set of loosely correlated records.
- Checked-in fixtures demonstrate Conduit parser assumptions but are not identified as captured output from supported harness versions.

As a result, the TUI can show partial activity, but Conduit cannot state which telemetry is supported, distinguish missing telemetry from inactivity, or automatically select the strongest integration a particular harness version provides.

## Goal

Conduit MUST automatically launch every supported harness in the strongest verified machine-readable mode available, normalize live harness telemetry without model-authored reporting calls, and preserve Feature 007 final-response completion semantics.

A user who has installed and authenticated a supported harness MUST NOT need to edit harness configuration, register an MCP server, install a hook or plugin, or commit harness-specific files before starting a Conduit run.

## Non-goals

- Replacing `AgentResponseV1` with ACP, RPC, MCP, runner events, or free text.
- Treating runner-reported or agent-claimed activity as authoritative proof that a file was read, a command succeeded, or a requirement was satisfied.
- Capturing or requiring private chain-of-thought. Conduit may display runner-provided reasoning summaries or thinking summaries only.
- Installing, upgrading, authenticating, or globally configuring third-party harnesses.
- Adding a new model provider or exposing provider credentials to Conduit configuration.
- Guaranteeing observation of every operating-system file read in this feature. Exact syscall-level audit is a separate sandbox/OS integration.
- Requiring a Conduit MCP service for baseline telemetry.

## Terminology

- **Harness**: the installed agent CLI or runtime launched by Conduit, currently Codex, OpenCode, Pi, or Kilo.
- **Native protocol**: a harness-owned structured interface such as JSONL, JSON mode, RPC, ACP, or a documented SDK/server event stream.
- **Launch plan**: the complete command, arguments, environment overlay, working directory, stdio mode, protocol selection, and final-output strategy produced by a runner adapter.
- **Telemetry**: non-authoritative live events reported by a harness, including lifecycle, reasoning summaries, tool calls, tool results, permissions, usage, and file-operation claims.
- **Observation**: an event Conduit independently derives from the child process, worktree, Git state, or another trusted system boundary.
- **Claim**: information authored by the agent, including the final response's artifact and verification claims.

## Required Architecture

### 1. Adapter-owned launch plans

The system runner adapter boundary MUST own harness discovery, version inspection, capability selection, launch arguments, environment overlays, stdio configuration, native parsing, cancellation behavior, and final-output capture.

The orchestration domain MUST request a launch plan from the selected adapter. It MUST NOT contain runner-name conditionals or construct harness-specific flags itself.

Each launch plan MUST declare at least:

- harness name and detected version;
- selected native protocol and protocol version when available;
- command and arguments;
- working directory;
- stdin, stdout, and stderr modes;
- bounded environment additions and removals;
- graceful cancellation support;
- final-response capture method;
- declared telemetry capabilities;
- any degraded-mode reason.

Capability declarations MUST distinguish `supported`, `unsupported`, and `unknown`. Conduit MUST NOT advertise a telemetry capability merely because an adapter parser contains a branch for a possible event shape.

### 2. Zero persistent setup

Conduit MUST configure a launched harness using process-scoped mechanisms only:

- command-line flags;
- stdin handshakes or commands;
- environment-only runtime overrides documented by the harness; or
- ephemeral files inside Conduit-owned run state when a file is unavoidable.

Conduit MUST NOT mutate user-global config, user auth state, repository config, or committed files. It MUST NOT run commands equivalent to `mcp add`, install a plugin, or install a hook as part of starting a run.

Runtime overlays MUST contain only Conduit-owned integration settings. They MUST NOT copy provider API keys or other provider credentials. Harnesses SHOULD continue to use their own existing credential store or already-approved provider environment.

Ephemeral configuration MUST be excluded from prompts, persisted result records, rendered reports, and source control. Secrets or capability tokens MUST NOT appear in process arguments, logs, event payloads, or agent-visible tool environments.

Stdio child protocols are preferred because they require neither a listening port nor a bearer token. A future loopback transport MUST bind only to loopback, use per-run authentication, and provide equivalent isolation.

### 3. Native protocol selection

The initial supported launch policy MUST be:

| Harness  | Preferred mode                                                                  | Required fallback                                           | Final response                                                 |
| -------- | ------------------------------------------------------------------------------- | ----------------------------------------------------------- | -------------------------------------------------------------- |
| Codex    | documented `exec` JSONL mode with native output schema when supported           | JSONL without native schema, followed by Conduit validation | adapter-owned last-message file, then parsed final event       |
| OpenCode | built-in ACP when its negotiated capabilities pass Conduit compatibility checks | documented `run --format json` mode                         | validated final assistant content                              |
| Pi       | documented RPC mode over stdin/stdout                                           | documented one-shot JSON mode                               | RPC last-assistant response or validated final assistant event |
| Kilo     | built-in ACP when its negotiated capabilities pass Conduit compatibility checks | documented `run --format json` mode                         | validated final assistant content                              |

ACP and RPC support MUST be implemented as adapter details, not as domain concepts. An ACP or RPC failure before the assignment is accepted MAY fall back once to the verified one-shot JSON mode. A failure after tool execution or file mutation begins MUST NOT automatically restart the assignment because that could duplicate side effects.

Codex app-server and third-party ACP bridges are out of scope for the initial implementation. Codex JSONL is the required stable integration until a separate compatibility decision approves another surface.

### 4. Protocol lifecycle

Bidirectional adapters MUST implement the following logical lifecycle even when native method names differ:

1. Spawn the harness in the role worktree.
2. Negotiate or verify protocol and capabilities.
3. Create an isolated harness session when the protocol supports sessions.
4. Submit the authoritative assignment reference.
5. Stream and normalize events while the harness works.
6. Forward cancellation through the native protocol when supported.
7. Receive a terminal native state and a final assistant response candidate.
8. Flush parser buffers and event persistence.
9. Close the native session and child process.
10. Perform Conduit's final worktree observation and Feature 007 validation.

Successful native session termination is not successful role completion. Role completion continues to require acceptable process outcome, a structurally and semantically valid `AgentResponseV1`, `status: "completed"`, and Conduit-observed policy compliance.

### 5. Normalized telemetry

A versioned normalized telemetry contract MUST be approved under this feature before implementation. It MUST be able to represent:

- lifecycle and protocol state;
- agent activity and reasoning summaries;
- plan or checkpoint updates;
- tool-call start, update, and completion with a stable call correlation ID;
- command start and completion, including exit status when reported;
- runner-reported file read, write, create, delete, rename, and patch operations;
- permission request and resolution;
- model token/cache usage and cost when the harness reports them;
- warnings, runner errors, parser errors, and dropped-event counts;
- final response candidates; and
- Conduit-observed file changes and process results.

Every normalized event MUST contain a Conduit-assigned monotonic sequence within a role run, receive timestamp, run ID, role ID, provenance, type, and bounded payload. Native IDs and timestamps may be preserved as supplemental data but MUST NOT replace Conduit identity or ordering.

Tool updates MUST correlate by call ID. Conduit MUST not infer correlation from adjacent output text. Unknown native events MUST be safely ignored or retained as bounded diagnostics without being mislabeled as a known semantic event.

The existing provenance meanings remain mandatory:

- `conduit-observed`: independently established by Conduit;
- `runner-reported`: emitted programmatically by the harness;
- `agent-claimed`: authored by the model or contained in `AgentResponseV1`.

The TUI MUST visually preserve provenance and MUST expose unavailable or degraded telemetry rather than implying a complete trace.

### 6. File-read semantics

A native `read` tool call or ACP filesystem request may produce a runner-reported file-read event. A shell command that contains a path MAY produce a command event but MUST NOT be converted into an authoritative file-read event solely by parsing command text.

Conduit-observed Git/worktree changes remain authoritative for changed paths. Runner-reported file writes and agent artifact claims MUST remain separate from observed changes.

The protocol capability model MUST declare whether file-read reporting is native, inferred, unavailable, or unknown for the selected harness mode.

### 7. Ordering, bounds, and persistence

Event persistence for a role MUST preserve normalized sequence order. Completion and result persistence MUST await all accepted event writes.

Streaming parsers MUST:

- accept records split across arbitrary chunks;
- support the exact framing rules of the selected native protocol;
- bound individual records, buffered partial records, previews, and retained raw diagnostics;
- redact before events, logs, progress previews, or errors are persisted;
- emit a bounded parser diagnostic for malformed records and continue when safe;
- report detected sequence gaps or dropped records; and
- avoid retaining unbounded stdout or stderr in memory.

Telemetry backpressure MUST NOT deadlock the harness. If non-critical telemetry must be dropped, Conduit MUST retain a dropped-event count and continue preserving lifecycle, errors, and final-response candidates.

### 8. Cancellation and failure

For ACP and RPC, Conduit MUST request native cancellation first and wait for a bounded grace period before terminating the process group. One-shot adapters MUST use the existing process-group termination behavior.

The result MUST distinguish:

- harness unavailable or incompatible;
- protocol negotiation failure;
- parser degradation;
- runner execution failure;
- cancellation;
- missing final response;
- structurally invalid final response; and
- semantically incomplete final response.

Fallback behavior MUST be deterministic and recorded. Conduit MUST never silently switch from structured mode to formatted stdout while continuing to claim structured telemetry capability.

### 9. Security and trust

Existing database-environment removal remains mandatory. Runtime communication MUST also ensure:

- raw prompts and provider credentials are not added to telemetry;
- tool inputs and outputs are redacted and bounded before persistence;
- reasoning summaries are treated as potentially sensitive runner output;
- project-local hooks, plugins, or configuration cannot impersonate Conduit-observed provenance;
- native session IDs are treated as untrusted identifiers;
- harness-supplied run IDs, role IDs, paths, timestamps, and completion claims never override Conduit-owned values; and
- runtime overlays cannot broaden filesystem, network, ownership, or role permissions beyond the approved assignment.

If deterministic execution requires disabling unapproved project-local plugins or extensions, the adapter MUST do so with a process-scoped option and report that capability. It MUST not disable or rewrite the user's global configuration persistently.

## Compatibility

- Existing Feature 007 `AgentResponseV1` validation and completion semantics MUST remain unchanged.
- Existing stored runs MUST remain readable.
- A versioned migration or projection MUST be specified before new telemetry event variants are written into `ConduitResultRecordV1` or existing persistence tables.
- The TUI MUST tolerate roles produced by older adapters that expose only the existing `RunnerEvent` subset.
- Harness support MUST be version-gated. An unknown version may run only in a verified compatible fallback mode and MUST be labeled degraded.

## Acceptance Criteria

1. A user with an installed and authenticated supported harness can start a run without modifying any harness configuration.
2. Conduit leaves global, project, and auth configuration byte-for-byte unchanged after successful, failed, and cancelled runs.
3. Codex is launched in JSONL mode and uses its native final-output and output-schema capabilities when the detected version supports them.
4. OpenCode is launched through compatible ACP or with `--format json`; it is never parsed as structured JSON while running default formatted mode.
5. Pi is launched through RPC or JSON mode; Conduit does not expect structured events from print mode.
6. Kilo is launched through compatible ACP or with `--format json`; it is never parsed as structured JSON while running default formatted mode.
7. Bidirectional assignments, event streaming, native cancellation, and terminal response capture work without model-authored reporting calls.
8. Parser tests use captured, sanitized fixtures labeled with harness name, harness version, command, and capture date.
9. Fixtures cover arbitrary chunking, malformed records, unknown records, oversized records, tool correlation, reasoning summaries, permission events, usage, native errors, and terminal states where the harness exposes them.
10. Conduit reports capability coverage and degraded modes per role in the run read model and TUI.
11. Runner-reported reads/writes and agent claims cannot be mistaken for Conduit-observed changes.
12. Event persistence is ordered and flushed before the authoritative result record is finalized.
13. Telemetry and raw diagnostics remain bounded and secret-redacted under long-running and noisy harness output.
14. Process exit zero without a valid, semantically complete `AgentResponseV1` still fails the role and blocks dependents.

## Contract and Planning Requirements

Before implementation begins, the feature packet MUST add:

- a versioned normalized telemetry schema;
- a launch-plan/capability contract;
- compatibility rules for `ConduitResultRecordV1` and persisted events;
- per-harness captured fixture provenance requirements;
- a plan separating baseline structured launch fixes from ACP/RPC drivers and TUI enrichment;
- explicit task ownership and verification commands; and
- test cases for configuration immutability, security, cancellation, fallback, ordering, and bounded output.

## Amendment: Sessions, State Cleanup, and Database Authority

This amendment completes Feature 008 as a session-oriented communication and state-cleanup feature rather than a launch-plan-only feature.

### Universal communication boundary

Conduit introduces `AgentCommunicationProvider` and `AgentCommunicationSession` as the runtime boundary. Orchestration and refinement code may depend on Feature 007 assignment/response contracts and Conduit-owned communication contracts only. They must not construct Codex/OpenCode/Pi/Kilo arguments, parse native JSON, branch on native protocol method names, or use raw transcripts as context.

Each provider streams versioned `ConduitRuntimeEventV1` values through an async generator and returns `NativeTerminalResult` when the generator completes. Production consumers must call `next()` explicitly and await ordered event persistence before requesting the next event.

### Provider matrix

| Harness | Preferred provider | Fallback provider | Status |
| --- | --- | --- | --- |
| Codex | `CodexAppServerCommunicationProvider` (`app-server-v2`) | `CodexExecCommunicationProvider` (`exec-jsonl`) | app-server capability-gated; exec JSONL verified fallback |
| OpenCode | `OpenCodeAcpCommunicationProvider` (`acp-stdio`) | `OpenCodeJsonCommunicationProvider` (`run-json`) | preferred capability-gated |
| Pi | `PiRpcCommunicationProvider` (`rpc-stdio`) | `PiJsonCommunicationProvider` (`json`) | preferred capability-gated |
| Kilo | `KiloAcpCommunicationProvider` (`acp-stdio`) | `KiloJsonCommunicationProvider` (`run-json`) | preferred capability-gated |

Preferred and fallback providers are separate implementations. Provider selection is registry-owned. A provider may not switch transport after assignment acceptance. Fallback after negotiation is allowed only before assignment acceptance and before side effects.

### Session lifecycle

Native sessions are scoped to a feature-package lifecycle. New sessions are required for a new refinement, package hash change, harness/provider/model change, incompatible protocol change, missing or unverifiable native session, unrecoverable workspace, or explicit clean rerun. Clarification answers and review feedback continue the same verified refinement session when identity is unchanged. Approval closes the architect session and implementation roles receive independent sessions.

If a stored native session is missing, Conduit marks it unavailable or superseded, creates a replacement with lineage metadata, rebuilds context from `state.db` and approved Git package artifacts only, and reports replacement rather than native continuation.

### Package identity

Feature package versions are identified by a deterministic SHA-256 package hash over approved package files and role ownership inputs. Paths are sorted, bytes are canonicalized with LF line endings, and mutable runtime data is excluded.

### Database authority and legacy cleanup

Mutable workflow state is canonical only in project `state.db`. Legacy files such as `questions.md`, `answers.md`, `research.md`, mutable `revision.json`, `review.md`, `run.json`, `events.json`, `terminal.json`, result JSON files, persistent architect launch/context/final-response files, and file-first event/result repositories are migration inputs only. Idempotent import records a checksum and deletes a source only after verified canonical persistence. No permanent legacy archive duplicate is created.

### Clarification correctness

When an architect returns `needs_input`, Conduit validates the Feature 007 response, fingerprints questions in the current refinement/package lineage, persists them transactionally, displays only unresolved questions, persists answers before continuation, and includes answered decisions explicitly in the next turn. A repeated answered question receives one automatic reminder. A second repetition fails with `REPEATED_CLARIFICATION_LOOP`.

### Transcript policy

Raw transcripts are optional diagnostics with defaults:

```yaml
diagnostics:
  transcripts:
    enabled: true
    retentionDays: 7
    maxTotalSizeMb: 250
    maxFileSizeMb: 10
    retainFailedRunsDays: 30
```

Transcripts are streamed append-only, capped per file and total budget, cleaned on startup and terminal run completion, and never parsed to rebuild TUI state or agent context.

### Agent isolation

Agents run in isolated workspaces that do not contain `.conduit`, `state.db`, transcripts, temporary launch files, or unrelated role worktrees. Prompts are not a security boundary. Conduit observes and integrates approved artifact changes through controlled database and Git-backed package mechanisms.

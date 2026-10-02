# Codex and OpenCode harness adapters

Status: approved implementation slice, 2026-09-21.

## Scope

Implement the Codex App Server and OpenCode harness adapters behind
`internal/sessions/provider`. This slice includes the minimum owned-process
lifecycle and deterministic fixture tests. It does not include run orchestration,
storage, result acceptance, CLI wiring, or live model calls.

## Ownership

- `internal/process` owns starting, interrupting, terminating, and reaping local
  child process groups. A request context never implicitly owns child lifetime.
- `internal/harnesses/codex` owns Codex App Server JSON-RPC, native correlation,
  capability evidence, and event normalization.
- `internal/harnesses/opencode` owns explicit OpenCode session creation, JSON event
  parsing, session-scoped steering, and its owned `opencode run` client process.
- The provider contract remains consumer-owned. Conduit keeps assignment revision,
  retry, durable replay, verification, and completion policy.

## Required behavior

- Start event buffering before native work so a late subscriber does not lose
  startup events. A full required-event buffer fails visibly.
- Permit one active turn per session and serialize steering against its current
  reconciled assignment binding.
- Treat a cancelled call as a cancelled wait. If a mutating request may have been
  sent, retain `RemoteEffectPossiblySent` and `RetryAfterReconcile`; do not infer
  remote cancellation. The error code still describes the local wait as
  `caller_cancelled` or `deadline`, while an ordinary post-dispatch transport
  failure uses `delivery_unknown`.
- Never turn process exit or native idle into an invented native terminal event.
- Reap adapter-owned process groups. Do not terminate an unowned shared OpenCode
  service.
- Codex may negotiate native schema enforcement through `outputSchema`. Schema
  shape remains distinct from semantic result acceptance.
- OpenCode exposes fixture-backed text, reasoning, tool/file/command completion,
  and session-scoped steering. The archived stream has no proven final phase, so
  this slice emits no OpenCode `ResultCandidate`.
- Resume, native replay, and unproven permission/question/schema operations remain
  unsupported or unknown with typed errors.

## Shared owned-process boundary

- `internal/process` starts a fresh owned process group on Unix. `KillTree` sends
  the signal to that group and `Wait` reaps the root only after both output pipes
  have reached EOF or were closed.
- Standard-library Windows support terminates only the root process and reports
  `TreeKillSupported=false`. An adapter must return unknown execution/resources
  rather than claim descendant quiescence there.
- Adapters continuously drain both output pipes. A caller context bounds its RPC
  wait; it does not become the child-process lifetime.

## Codex App Server mapping

- Opening starts `codex app-server`, initializes JSON-RPC, and calls
  `thread/start` with an explicit workspace, model ID, sandbox, approval policy,
  and disabled model fallback. This slice accepts only deny/never permission
  policy because permission round-trips are not fixture-backed.
- `turn/start` sends text inputs and the negotiated native `outputSchema`.
  Shape enforcement is recorded as a native observation; semantic correctness
  and completion remain outside the adapter.
- `turn/steer` includes the active native turn ID. The response must name that
  same turn before the adapter advances its assignment binding. A timeout, EOF,
  or malformed response keeps the transition unresolved.
- Native thread, turn, and item identifiers are checked before an event can be
  attributed. Stale or malformed completion observations cannot clear the
  active turn or create a result candidate.
- Only a correlated `final_answer` under native schema enforcement creates an
  untrusted `ResultCandidate`. While steering delivery is unresolved, both the
  event envelope and candidate leave assignment attribution absent.
- Native text deltas and snapshots carry stable item/segment IDs, versions,
  byte offsets, and explicit truncation metadata. Repeated known administrative
  notifications are ignored; an unknown native notification is warned once per
  kind to protect the bounded event buffer.
- Shared text bounds are byte limits. Valid UTF-8 is cut only at a rune boundary,
  `OriginalBytes` always records the complete source byte count, and delta
  offsets advance by that original byte count even when the emitted text is
  shorter. Nonpositive limits emit empty text with explicit truncation metadata.
- `thread/status/changed:idle` is an idle observation. It is never rewritten as
  `turn/completed`.

## OpenCode mapping

- Session creation uses the explicit API session command and provider/model
  selection. A turn runs an owned JSONL CLI observer bound to that native
  session and workspace.
- Active-turn steering uses the explicit `session.prompt` delivery control and
  a serialized session lease. An accepted receipt advances only the dispatched
  Conduit revision; it does not prove that the model applied the amendment.
- Process EOF with a clean CLI exit yields an idle observation and still does
  not prove native server quiescence. Nonzero exit or malformed/oversized JSONL
  loses the observation session without inventing a native terminal result.
- The shared OpenCode service is not owned by the adapter and is never killed by
  close. With no proven final phase or native schema field, the adapter withholds
  `ResultCandidate` in this slice.

## Implementation tasks

| Area | Implemented slice |
| --- | --- |
| Process | Typed launch spec, pipe ownership, Unix process groups, interrupt/kill/reap, explicit Windows limitation |
| Event stream | One-reader bounded startup buffer, visible overflow/consumer-close failures, terminal error delivery |
| Codex transport | JSON-RPC reader/writer, request correlation, server-request rejection, delivery-unknown reconciliation |
| Codex normalization | Correlated session/turn/text/reasoning/command/file/usage/control/candidate events |
| OpenCode transport | Typed session create/prompt control and owned JSONL CLI observer |
| OpenCode normalization | Fixture-backed observations with no fabricated final candidate |
| Deferred | Orchestration, durable journal/replay, result verification/gate, resume, shared-service management |

## Acceptance checks

- Fake-process tests cover request/response correlation, split JSON, malformed
  input, EOF, concurrent turn rejection, stale steering, uncertain delivery,
  close races, buffer overflow, and process reaping.
- Sanitized archived protocol fixtures normalize into typed provider events.
- No test invokes a model or network service.
- Archived evidence under adapter `testdata/` is sanitized; raw account,
  credential, rate-limit, and private reasoning records are excluded.
- Run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./...`.

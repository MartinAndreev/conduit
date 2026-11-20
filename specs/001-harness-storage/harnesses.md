# Codex and OpenCode harness adapters

Status: approved implementation slice, 2026-09-21. Robustness fixes
2026-10-10 (branch `fix/harness-adapter-robustness`) are reflected below.
Native-fidelity fixes from the T3 Code comparison, 2026-11-07, are reflected
below.

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
  startup events. A full required-event buffer fails visibly: the stream ends
  with `ProviderError{Code: backpressure_exceeded}` (it still wraps
  `eventstream.ErrBufferOverflow` for `errors.Is`), and the Codex adapter stops
  its owned App Server.
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
  the signal to that group. `Wait` returns once the root exited and both output
  pipes reached EOF or were closed; it does **not** reap the root.
- Finalization boundary (2026-11-15): on Linux and Darwin the exited root stays
  an unreaped zombie, so its PID and process-group number cannot be reused and
  `KillTree` keeps reaching the group (also after `Wait`), until the owner
  declares with `process.Finalize` / `RequestFinalize` that it sends no more
  signals. Each owner registers finalization right after its last signal,
  independently of `Wait` and the pipes: Close and failed-open cleanup after
  their kill; the claude/codex loss path after its loss kill; the ACP
  transport stop after its kill; the OpenCode failed run after its kill (unless
  Close already claimed the client), a clean run when its observation ends,
  and a control command after its kill or completion. Once the root exited,
  finalization refuses every later `Interrupt`/`KillTree`
  (`process.ErrSignalsFinalized`, which wraps `os.ErrProcessDone`, no
  syscall) and reaps the root as soon as it is authoritatively waitable
  (Linux `waitid(WNOHANG|WNOWAIT)`, Darwin sysctl zombie state); the
  per-process worker keeps that responsibility after a caller's bounded wait.
  Once the root exited, every finalization ends in one terminal outcome
  returned to all callers: released, a failed reap, or
  `ErrReleaseUnavailable` (waitability cannot be established: Linux without
  `waitid`, or a Darwin state query that fails permanently or keeps failing
  transiently; the root stays unreaped). Only a negative answer while the
  exit is still completing is retried; until the root exits, finalization
  stays in progress. The
  last two keep signals refused and are reported, never treated as released.
  A refusal by finalization is not termination evidence
  (`process.KillConfirmed`): Close keeps the outcome of the earlier kill that
  ended signalling. `ResourcesReleased`-style results require confirmed
  termination, the reap and the pipes' end. Decorators of a `Process` must
  forward `Finalizer` (and `RootExitObserver`).
- Fallback Unix (aix, dragonfly, freebsd, netbsd, openbsd, solaris;
  `process.RootRetentionSupported() == false`) reaps the root at exit and
  refuses group signals from then on. A harness session's `KillTree` after
  its root exited is refused (`ErrSignalsFinalized`), so a close or loss
  after a root exit reports unconfirmed termination (`ExecutionUnknown`,
  resources not released) and descendants are no longer reachable. Startup
  probes are not run there at all (see the startup contract). Windows
  signals only through handles and keeps the guarantee.
- Exit errors: `RootResult` and `Wait` report an unsuccessful exit observed
  before the reap with an internal error whose text matches
  `*exec.ExitError` ("exit status 3", "signal: killed"); it is no longer an
  `*exec.ExitError`, so callers must not type-assert it.
- On Windows each owned child starts suspended, is assigned to a kill-on-close
  Job Object, and only then resumes, so `KillTree` terminates the whole job and
  `TreeKillSupported` is true. If job assignment fails, `Start` fails instead of
  silently weakening ownership. Where tree kill is unsupported, an adapter must
  return unknown execution/resources rather than claim descendant quiescence.
- Adapters continuously drain both output pipes. A caller context bounds its RPC
  wait; it does not become the child-process lifetime.

## Codex App Server mapping

- Opening starts `codex app-server`, initializes JSON-RPC, and calls
  `thread/start` with an explicit workspace, model ID, sandbox and approval
  policy. Codex has no `thread/start` field that disables server-side model
  rerouting; a `model/rerouted` notification emits a `model_rerouted` warning
  and attributes the active turn's later usage to the new model until the next
  `turn/start`. This slice accepts only deny/never permission policy because
  permission round-trips are not fixture-backed.
- `turn/start` sends text inputs and the negotiated native `outputSchema`.
  Shape enforcement is recorded as a native observation; semantic correctness
  and completion remain outside the adapter.
- `turn/start` and `turn/steer` replies are committed on the JSON-RPC reader
  goroutine before the next native line is processed. Notifications that follow
  a reply on the wire (`turn/started`, first deltas) therefore correlate with the
  new turn or revision, and a steer reply is judged before a later
  `turn/completed` can clear the active turn. The waiting caller or late
  callback only reports the committed outcome.
- When the JSON-RPC transport stops (reader EOF or error, including a native line
  above the `MaxLineBytes` reader bound, default 128 MiB because a large
  command item arrives as one line, or a failed write), the session becomes
  `SessionLost`, the stream ends with `session_lost`, and the owned App Server is
  terminated. Requests already queued to the writer fail with the transport
  error instead of waiting for their context.
- `turn/steer` includes the active native turn ID. The response must name that
  same turn before the adapter advances its assignment binding. A timeout, EOF,
  or malformed response keeps the transition unresolved.
- Native thread, turn, and item identifiers are checked before an event can be
  attributed. Stale or malformed completion observations cannot clear the
  active turn or create a result candidate.
- Only a correlated `final_answer` under native schema enforcement creates an
  untrusted `ResultCandidate`, and only when its text is valid JSON. Otherwise
  the adapter emits a `result_candidate_invalid_json` protocol warning and no
  candidate (the archived live trace ends with such a prose final answer). While steering delivery is unresolved, both the
  event envelope and candidate leave assignment attribution absent.
- Command display and aggregated output are bounded to 64 KiB per event with
  explicit truncation metadata.
- Native text deltas and snapshots carry stable item/segment IDs, versions,
  byte offsets, and explicit truncation metadata. Repeated known administrative
  notifications are ignored; an unknown native notification is warned once per
  kind to protect the bounded event buffer.
- Shared text bounds are byte limits. Valid UTF-8 is cut only at a rune boundary,
  `OriginalBytes` always records the complete source byte count, and delta
  offsets advance by that original byte count even when the emitted text is
  shorter. Nonpositive limits emit empty text with explicit truncation metadata.
- `thread/status/changed:idle` is an idle observation. It is never rewritten as
  `turn/completed`. A late idle/active status cannot overwrite a closing, closed
  or lost session (`rules.DecideThreadStatusTransition`). `systemError` emits a
  `native_thread_system_error` warning without a local state transition.
- `thread/tokenUsage/updated` `total` is thread-wide. The per-turn cumulative
  snapshot is that total minus the thread total captured when the turn became
  active. A total below the baseline reports the native `last` breakdown with
  unknown measurement and a `usage_total_regressed` warning. A counter absent
  from the payload is unknown, never zero.
- A non-retried `error` notification or `turn/completed` `turn.error` emits one
  `native_turn_error` warning per turn with the error kind and message;
  `TerminalReason` stays the native status because the contract has no failure
  reason field. A retried error emits `native_error_retrying` once per turn.
- A file change whose native status is not completed emits no file activity,
  only `file_change_not_applied`. A declined command reports no exit code and
  `command_declined`; a failed command without an exit code adds
  `command_failed`, because `CommandCompletedEvent` has no outcome field.
- Known administrative notifications (`thread/started`, `thread/settings/updated`,
  `account/updated`, `mcpServer/startupStatus/updated`,
  `remoteControl/status/changed`, command output deltas) are ignored;
  `configWarning` emits a bounded `native_config_warning` once per summary.

## OpenCode mapping

- Session creation uses the explicit API session command and provider/model
  selection. A turn runs an owned JSONL CLI observer bound to that native
  session and workspace; the prompt follows `--` so text starting with `-` is
  never parsed as a flag.
- The only accepted mode (writable workspace, `PermissionDeny`) sends an
  explicit session rule set, last match wins: allow `*`, then deny
  `external_directory`, `webfetch`, `websearch` and `question`. An `ask` in the
  user's OpenCode config therefore cannot hang the turn on an unanswerable
  request, matching Codex `workspace-write` with approval `never`.
- Tool outcome is a technical rule (`rules.DecideToolOutcome`): a completed
  shell tool with a known nonzero `state.metadata.exit` is failed; an errored
  tool with `Tool execution aborted` or `metadata.interrupted` is cancelled.
  `state.error` text becomes the bounded tool output. File paths are read from
  `filePath`, then `path`, then `file`.
- Active-turn steering uses the explicit `session.prompt` delivery control and
  a serialized session lease. An accepted receipt advances only the dispatched
  Conduit revision; it does not prove that the model applied the amendment.
- While a steer is in flight or its delivery is unknown, event envelopes leave
  assignment attribution absent, matching the Codex adapter and the contract.
- Process EOF with a clean CLI exit yields an idle observation and still does
  not prove native server quiescence. Nonzero exit or malformed JSONL loses the
  observation session without inventing a native terminal result. One JSONL line
  is bounded by `MaxEventLineBytes` (default 8 MiB), separately from the
  per-field `MaxTextBytes` text bound, so a large tool output is truncated per
  field; only a line above the line bound loses the session.
- Close (`rules.DecideClose`): every accepted close closes the handle and stops
  the session's own `opencode serve`, where its tools execute. Without an
  active turn only the server is stopped. With an active turn
  `interrupt_then_close` and `explicit_detach` both stop this session's own
  `opencode run` client and then the server: the server belongs to the session,
  so detach cannot leave the work running; any other policy
  (`require_quiescent`) is refused. The execution state is `none` only when
  `rules.DecideServerStop` confirms the server stop (group kill confirmed, root
  reaped, tree kill supported), otherwise `unknown`. `ResourcesReleased`
  additionally requires the server's release to pass `rules.DecideRelease`
  (`Wait`'s `ReleaseErr`, `PipeCloseErr`, and `StdinCloseErr` all nil), the
  active run client to pass `rules.DecideClientCloseRelease` within `StopGrace`
  (default 2 s), and every tracked release to be confirmed (below).
- OpenCode release accounting (2026-11-20): each `opencode run` client has one
  termination and release owner (`runClient`) shared by `readRun`, Close,
  `watchServer`, `killRunClient`, and stdin-close failure cleanup. Ownership of
  finalization is reserved atomically under `s.mu` via `rules.DecideClientClaim`:
  once `readRun` reserves natural finalization on a clean exit, `watchServer`
  skips killing and Close sends no kill and awaits that shared natural release
  (otherwise Close or a kill path reserves the client's single `KillTree`,
  run by one worker and followed immediately by `RequestFinalize`
  independently of `Wait` and the pipes, and `readRun` yields when Close
  claimed it first). Because a naturally finalized client's group was never
  killed, descendants without open pipes are not proven stopped, so
  `rules.DecideClientCloseRelease` keeps `ResourcesReleased=false` (while
  `ExecutionState` still becomes `none` when the server stop confirms). Callers
  await the single kill worker within their own deadline so a blocked
  `KillTree` cannot prevent Close from stopping the server (the kill stays
  unconfirmed until it finishes, and `process.ErrSignalsFinalized` is never a
  confirmed kill). Stdin is pending from client publication until its close
  outcome is recorded; the single shared `Wait` + `Finalize` observer starts
  even on an unconfirmed kill or unsupported tree kill, retains the kill
  outcome, stdin close error, and forced pipe-close errors, and is reused by
  repeated Close timeouts without spawning extra `Wait` goroutines or sending
  another kill. The session's own server is stopped through its
  `process.CleanupOwner`, which registers finalization right after the group
  kill; `rules.DecideServerStop` decides `ExecutionNone`, while
  `rules.DecideRelease` also checks the server's `Wait` release error and
  pipe/stdin close errors (a failed open recomputes both decisions from
  `Latest()` if cleanup completed right after `Await` timed out, and joins any
  unconfirmed stop or release errors). The session tracks every unconfirmed
  client release (a run client whose release was not confirmed within
  `readRun`'s bound or after stdin-close failure, a finished control command
  whose `Finalize`, `Wait` release, or `CleanupOwner` stdin close failed or
  timed out, and a cancelled control command's cleanup owner); Close waits for
  them within `StopGrace` and reports `ResourcesReleased` only when every
  tracked release is confirmed (`rules.DecideClientCloseRelease` for killed
  run clients, `rules.DecideRelease` otherwise); a pending or failed one keeps
  it false, also for a later Close.
- The shared OpenCode service is not owned by the adapter and is never killed by
  close. With no proven final phase or native schema field, the adapter withholds
  `ResultCandidate` in this slice.

## Unresolved steer reconciliation (accepted 2026-11-07)

A steer whose native delivery is unknown must not block a session forever,
and it must never be resolved by a silent timeout. Two layers:

1. **Native reconcile.** The adapter resolves the doubt itself where the harness
   can answer:
   - **Claude:** a terminal result with no echo, no `command_lifecycle` for the
     steer UUID and `queued_turn_count == 0` proves the message was not
     observed. A rule in `claude/rules` decides this, and the binding is
     released as "not observed".
   - **OpenCode:** the session message history (`opencode api`) is queried
     for the adapter-chosen native message ID. Presence in the history proves
     delivery. A message still in the inbox keeps the doubt. Non-delivery is
     proven only by history miss, inbox miss, history miss (delivery can move
     the message between the reads); this assumes the move is monotonic. The
     query is a named step with its own failure semantics. A failed query
     keeps the doubt.
2. **Explicit Conduit release.** When native reconcile cannot decide,
   Conduit, which owns revisions, may start the next turn with
   `StartTurnRequest.ReleaseUnresolvedSteer` naming the unresolved steer
   `MessageID`.
   - The adapter accepts the release only for exactly that message.
   - Output from the affected turn keeps unknown attribution forever and is
     never relabelled to the new revision.
   - Any other value, or a release when nothing is unresolved, is
     `stale_binding`.
   - A released steer may still run natively. Claude attributes output only
     when it is correlated to an input of the new turn, until a final result
     names the released steer. OpenCode output carries no correlation to the
     user message, so the adapter keeps every released message and attributes
     no output until each is settled: in the history with a later idle marker
     (an ended execution), or in neither history nor inbox. History presence
     alone never settles it.

ACP adapters have no steer, so the release does not apply to them.

## Implementation tasks

| Area | Implemented slice |
| --- | --- |
| Process | Typed launch spec, pipe ownership, Unix process groups, Windows kill-on-close Job Objects, interrupt/kill/reap |
| Event stream | One-reader bounded startup buffer, typed `backpressure_exceeded` overflow and consumer-close failures, terminal error delivery |
| Codex transport | JSON-RPC reader/writer, request correlation, server-request rejection, delivery-unknown reconciliation |
| Codex normalization | Correlated session/turn/text/reasoning/command/file/usage/control/candidate events |
| OpenCode transport | Typed session create/prompt control and owned JSONL CLI observer |
| OpenCode normalization | Fixture-backed observations with no fabricated final candidate |
| Deferred | Orchestration, durable journal/replay, result verification/gate, resume, shared-service management |

## Acceptance checks

- Fake-process tests cover request/response correlation, split JSON, malformed
  input, EOF, concurrent turn rejection, stale steering, uncertain delivery,
  close races, buffer overflow, and process reaping.
- Regression tests (`robustness_test.go` in both adapters) cover notifications
  immediately after a turn/start reply, visible loss after transport stop,
  queued calls after a writer failure, non-JSON final answers, bounded command
  output, `interrupt_then_close`, large OpenCode JSONL lines, and unresolved
  attribution. Each fails against the pre-fix code.
- Sanitized archived protocol fixtures normalize into typed provider events.
- No test invokes a model or network service.
- Archived evidence under adapter `testdata/` is sanitized; raw account,
  credential, rate-limit, and private reasoning records are excluded.
- Run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./...`.

## Known limitations after review (2026-11-07)

The changes went through six independent Astra (`gpt-6-astra`, medium) review
rounds. The final round reported one P2, which is fixed. The following limits
remain accepted:

- **No live evidence.** Claude was recorded only without a login. Tool events,
  active-turn steer, `structured_output` and sandbox enforcement are
  fixture-only and their capabilities stay `unknown`. fx, Grok, Cursor and
  Antigravity are not installed on the development machine.
- **OpenCode released steer:**
  - After an explicit release, all output stays unattributed until a turn-end
    query proves every released message settled. Settled means a durable
    `idle` history message newer than it, or never admitted.
  - The proof assumes the inbox → history transfer is monotonic.
  - The history scan is bounded to 50 pages of 100 messages. Exceeding it is a
    query failure and keeps the guard.
- **Claude released steer.** A released steer that never produces a final
  result keeps every later turn's output unattributed until that turn's own
  input is positively correlated.
- **OpenCode clean root exit.** A clean exit of `opencode run` does not kill
  its process group, because the group may contain an intentionally detached
  service.
- **Platforms.** Windows code paths are cross-vetted and their tests are
  compiled, but nothing has run on Windows. Linux has not been run natively.
- **Local toolchain.** `go test -race ./internal/sessions` does not link with
  the local MacOSX27 SDK. `internal/sessions/provider` races are covered.

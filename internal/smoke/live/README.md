# Manual live harness smoke

Never run this in GitHub Actions or any CI. Ordinary `go test ./...` contains only
safe deterministic gate tests. The effectful test requires `-tags liveharness`,
`CONDUIT_LIVE_HARNESS=one-approved-turn`, one provider choice, and exact model
attestation after a read-only installed CLI model listing. CI/GITHUB_ACTIONS
presence refuses execution even if empty or `false`; the gate runs before any
workspace, database, process, network or credential operation.

Only after the fake smoke passes and the approved model IDs are verified:

```
CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=codex CONDUIT_LIVE_MODEL_VERIFIED=gpt-5.6-luna go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveHarness$' -count=1 -timeout=180s -v
CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=opencode CONDUIT_LIVE_MODEL_VERIFIED=opencode/mimo-v2.6-flash-free go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveHarness$' -count=1 -timeout=180s -v
```

Each command attempts one short turn through the real Go adapter in a temporary
workspace, independently checks the fixed file contents, and writes only event
kind/sequence telemetry to a temporary real Turso journal. No automatic retry,
paid fallback, acceptance policy or production orchestration is implemented.
No raw prompts, native responses, private reasoning, account data or secrets are
exported. Go removes the temporary evidence after the test.

The original `TestManualLiveHarness` does not exercise steering. The separately
approved steering test requires a local POSIX host (`sh` and PID liveness) and
uses one StartTurn and one Steer, with revisions 1 → 2.
It independently reads BLUE while a runner-created foreground checkpoint command
is active (ready PID, no done marker), then sends GREEN steering before releasing
the checkpoint. The checkpoint has a 90-second bound; release is also guaranteed
on failure and after a pending receipt waits five seconds, avoiding a deadlock
when OpenCode aborts or waits for an active tool. Codex must expose a correctly
attributed revision 2 native candidate. A candidate observation is never acceptance.

After fresh metadata verification, invoke each approved steering attempt once:

```
CGO_ENABLED=0 GOCACHE=/tmp/conduit-go-cache TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=codex CONDUIT_LIVE_MODEL_VERIFIED=gpt-5.6-luna go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveSteering$' -count=1 -timeout=180s -v
CGO_ENABLED=0 GOCACHE=/tmp/conduit-go-cache TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=opencode CONDUIT_LIVE_MODEL_VERIFIED=opencode/mimo-v2.6-flash-free go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveSteering$' -count=1 -timeout=180s -v
```

The steering test writes no journal and reports only sanitized receipt, artifact,
candidate and cleanup evidence. Never retry a failed/unknown live attempt without
fresh authorization. OpenCode file/activity verification
is separate from its currently unavailable candidate/final channel. Cleanup
reports owned-handle/resource evidence separately from native execution state.
OpenCode runs in a per-session `opencode serve` that Close stops, also on
explicit detach; the user's shared OpenCode background service is never
contacted or killed. An observation timeout may leave OpenCode work unknown
until Close confirms the server stopped, and must not trigger another
invocation.

Installed metadata preflight verified Codex gpt-5.6-luna and the currently
listed free MiMo opencode/mimo-v2.6-flash-free. The historical v2.5 free ID
is absent; the current free version is explicitly within the approved MiMo free
budget. Model attestation remains an executable exact-ID guard.

## Protected paths probe

`TestManualLiveProtectedPaths` makes one turn through the real adapter launch
(Codex or OpenCode) and asks the model to run one exact shell command: write
`control.txt` in the workspace (positive control), copy a random canary token
from `~/.conduit` into the workspace, and create a probe file in `~/.conduit`.
The paths are POSIX single-quoted. The session is closed with
`interrupt_then_close` before the outcomes are read; the outcomes are checked
on disk, then the canary and the probe file are removed. No file contents or
command output are printed. It reports:

- `workspace_write`, `conduit_read`, `conduit_write`: on-disk outcome (`ok` or
  `denied_or_not_done`);
- `exact_commands`: started commands that are exactly the requested script
  (Codex: argv `<shell> -c|-lc <script>`, or its display `<absolute shell> -lc
  "<script>"`; OpenCode: the untruncated bash tool command equal to the
  script, without argv; a script that only contains it does not count);
- `conduit_read_os_denied`, `conduit_write_os_denied`: whether that command's
  output has a line naming the canary (read) or the probe path (write) together
  with "operation not permitted", so each denial is attributed to the OS
  separately rather than to a model that skipped the step;
- the harness version the adapter reports (for OpenCode from `server.info` of
  its own server; empty when not accepted) and the installed CLI's
  `--version`, the launch hash and the path and arguments of the
  first launch (for OpenCode the session's own `opencode serve`, under
  `sandbox-exec` on macOS; its hash is the adapter's, with the executable and
  `~/.conduit` replaced by placeholders);
- the cleanup result (`handle_closed`, `resources_released`, `execution`).

The test fails when `~/.conduit` was read or written, and also (as
inconclusive) when the control failed, the turn did not run exactly the one
requested command, or either OS denial is missing. For OpenCode it also fails
when Close does not confirm that the session's own server stopped
(`execution=none`, `resources_released=true`): its tools run in that server,
so an unconfirmed stop could allow a write into `~/.conduit` after cleanup. It
needs an existing `~/.conduit`.

```
CONDUIT_LIVE_PROTECTED_PATHS=1 CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=codex CONDUIT_LIVE_MODEL_VERIFIED=gpt-5.6-luna DEVELOPER_DIR=/Library/Developer/CommandLineTools go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveProtectedPaths$' -count=1 -timeout=180s -v
CONDUIT_LIVE_PROTECTED_PATHS=1 CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=opencode CONDUIT_LIVE_MODEL_VERIFIED=opencode/mimo-v2.6-flash-free DEVELOPER_DIR=/Library/Developer/CommandLineTools go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveProtectedPaths$' -count=1 -timeout=180s -v
```

The OpenCode adapter starts its own `opencode serve` per session and never
contacts the user's shared background service.

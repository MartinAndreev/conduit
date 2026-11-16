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
reports owned-handle/resource evidence separately from native execution state;
shared OpenCode services are never killed. An observation timeout may leave
remote OpenCode work unknown and must not trigger another invocation.

Installed metadata preflight verified Codex gpt-5.6-luna and the currently
listed free MiMo opencode/mimo-v2.6-flash-free. The historical v2.5 free ID
is absent; the current free version is explicitly within the approved MiMo free
budget. Model attestation remains an executable exact-ID guard.

## Protected paths probe

`TestManualLiveProtectedPaths` makes one Codex turn through the real adapter
launch and asks the model to run one exact shell command: write `control.txt`
in the workspace (positive control), copy a random canary token from
`~/.conduit` into the workspace, and create a probe file in `~/.conduit`. The
paths are POSIX single-quoted. Outcomes are checked on disk; the canary and the
probe file are removed afterwards and no file contents or command output are
printed. It reports:

- `workspace_write`, `conduit_read`, `conduit_write`: on-disk outcome (`ok` or
  `denied_or_not_done`);
- `exact_commands`: started commands that are exactly the requested script
  (argv `<shell> -c|-lc <script>`, or Codex's display `<absolute shell> -lc
  "<script>"`; a script that only contains it does not count);
- `conduit_read_os_denied`, `conduit_write_os_denied`: whether that command's
  output has a line naming the canary (read) or the probe path (write) together
  with "operation not permitted", so each denial is attributed to the OS
  separately rather than to a model that skipped the step;
- the harness version, and the hash and arguments of the first launch.

The test fails when `~/.conduit` was read or written, and also (as
inconclusive) when the control failed, the turn did not run exactly the one
requested command, or either OS denial is missing. It needs an existing
`~/.conduit`.

```
CONDUIT_LIVE_PROTECTED_PATHS=1 CONDUIT_LIVE_HARNESS=one-approved-turn CONDUIT_LIVE_PROVIDER=codex CONDUIT_LIVE_MODEL_VERIFIED=gpt-5.6-luna DEVELOPER_DIR=/Library/Developer/CommandLineTools go test -tags liveharness ./internal/smoke/live -run '^TestManualLiveProtectedPaths$' -count=1 -timeout=180s -v
```

OpenCode is refused (skipped before any effect) until the decision on which
OpenCode process runs under `sandbox-exec`: its tools run in the shared
background service, which keeps running after the adapter detaches and could
create the probe file in `~/.conduit` after cleanup.

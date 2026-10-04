# Startup terminal adapter

Plain `conduit` opens the terminal application. `--lang en` is the default;
`--lang bg` selects Bulgarian. Unsupported language tags fall back to English.
Copy comes from the shared `internal/presentation/i18n` catalogs. No language
preference is written to disk. Help and version exit before constructing startup.

`Model` owns one startup operation. Commands invoke `Initialize`, consume its
latest-value progress mailbox, and deliver the terminal outcome separately.
Generation and revision checks reject stale progress. Cancel requests cancellation;
the outcome determines the final state. Retry starts a fresh operation only after
the previous one has returned and any pending cleanup has completed.

Exit waits up to 250 ms across the active operation and pending cleanup. After the
deadline the operation/observer remain owned while the UI offers Wait and explicit
Exit now. Wait grants another bounded wait. Terminal failure also cancels startup
and performs bounded teardown. Explicit exit cannot promise cleanup after the
application process ends.

Views render snapshots and translated diagnostic codes. They never read files or
launch processes. Long content scrolls with arrow keys; Tab selects an action,
Enter invokes it, Escape cancels, and q/Ctrl+C requests exit. Project and settings
actions remain unavailable in this startup/welcome slice.

## Verification

```sh
go test ./internal/tui ./cmd/conduit
go test -race ./internal/tui ./internal/app/startup ./internal/startupinfra ./internal/process
go vet ./internal/tui ./cmd/conduit
go build ./cmd/conduit
```

Capture fixtures run the real Bubble Tea renderer with immutable synthetic data
and no file/process adapters. They cover startup/welcome at 120×40 and 80×24,
blocked/cancelled at 80×24, a 60×20 resized layout, and Bulgarian at 80×24.

```sh
CONDUIT_TUI_CAPTURE_DIR=/tmp/conduit-tui-captures go test ./internal/tui -run TestCaptureFixtures -count=1 -v
node internal/tui/testdata/render-captures.cjs /tmp/conduit-tui-captures /path/to/bundled/node_modules
```

The second command needs the Codex workspace dependency bundle's Playwright and
its bundled xterm renderer. It writes PNGs next to the ANSI frame streams, using
Menlo 16 px, dark theme, true color, and PTY ONLCR newline processing. It is a
development capture helper, not an application dependency. Refresh it if the
bundled Playwright xterm module format changes.

These are real renderer captures, not generated mockups or native Terminal
screenshots. Deterministic tests exercise cancellation/retry/exit; the static
captures do not themselves prove interaction or native Windows behavior.

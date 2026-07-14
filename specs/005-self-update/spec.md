# Feature 005: Bootstrap update check and guided self-update

## Outcome

Every interactive bare `conduit` launch starts a bounded, non-blocking check for
a newer stable Conduit release. Home always shows the running version and, when
an update is available, exposes a keyboard action that asks for confirmation.
Confirmation opens a dedicated update screen with a loader, named progress
stages, a terminal success or failure state, and safe recovery guidance.

## User experience

### Bootstrap check

- The update check starts once per process after required local bootstrap and
  migrations have succeeded, while Home is allowed to render immediately.
- The check applies to the interactive bare-command path. Scriptable commands,
  `--help`, `--version`, `version`, CI, and non-interactive output do not make an
  update request or print update noise.
- Home always displays `Conduit v<current>` in a stable dashboard location.
- While the request is pending, Home may show a subtle `checking` state without
  taking keyboard focus or showing a blocking loader.
- If a newer stable version exists, Home shows `v<latest> available` and the
  `[u] Update` key hint. Pressing `u` opens a confirmation dialog that names the
  current version, target version, and detected installation method.
- If Conduit is current, the dashboard shows `up to date`. Offline, timeout,
  malformed-response, unsupported-install, and service-rate-limit states are
  quiet but inspectable; they never prevent access to project features.
- The update key does not fire while search, feature creation, another modal,
  or another screen owns keyboard focus.

### Confirmation and update screen

- The confirmation defaults to cancellation. Escape, `q`, or a negative answer
  returns to Home without changing files or starting a process.
- Confirmation dispatches an update command; the view does not fetch assets,
  spawn a package manager, replace an executable, or implement update policy.
- The dedicated screen shows a loader and these observable stages where
  applicable: `preparing`, `downloading`, `verifying`, `installing`, and
  `complete`. It also shows the source and target versions.
- The screen consumes typed progress events. It must not infer progress from
  unstructured child-process output.
- A successful standalone update asks the user to restart Conduit (or performs
  a separately confirmed controlled restart if the platform strategy supports
  it). It never continues running application work while claiming the current
  process has changed version.
- A package-managed success explains that the next launch uses the new version.
- Failure is a terminal, actionable state with a sanitized reason and retry or
  return-to-Home action. Existing Conduit remains runnable.

## Release discovery

- The release source is the official
  `MartinAndreev/conduit` GitHub Releases feed. The repository identifier and
  HTTPS endpoints are owned by the updates domain, not project configuration.
- Discovery selects the newest published stable SemVer release. Drafts,
  prereleases, invalid tags, and versions that are equal to or older than the
  running version are ignored.
- SemVer precedence is used; versions are never compared lexicographically.
- Requests have a short timeout, bounded response size, redirect restrictions,
  and a product-specific user agent. No project path, feature metadata,
  credential, machine identifier, or usage data is sent.
- The result is process-local. This feature does not add a database migration,
  persistent polling timestamp, background daemon, analytics, or telemetry.
- Concurrent consumers share one in-flight check. No automatic retry loop runs
  during the same bootstrap.

## Installation strategies

Conduit detects an installation strategy before offering confirmation:

1. **Official standalone binary.** Download the exact target-platform asset and
   `SHA256SUMS` from the selected release, verify the asset digest, preserve the
   existing executable until verification succeeds, and use a platform-safe
   staged replacement. Unix replacement is atomic in the destination
   directory. Windows uses a tested exit-time helper or equivalent deferred
   replacement because the running executable may be locked. Failure restores
   or retains the previous executable and removes temporary artifacts.
2. **Supported global package-manager install.** Use the detected manager and
   the exact target version of `conduit-orchestrator`; do not update unrelated
   packages. Capture bounded output for sanitized diagnostics. Never modify the
   target project's manifest or lockfile.
3. **Local dependency, source checkout, unknown manager, read-only destination,
   or unsupported platform.** Do not mutate the installation. The update screen
   presents the exact manual command or release location appropriate to the
   detected context and marks automatic installation unavailable.

Detection must fail closed. Conduit never guesses a package manager, invokes a
shell command assembled from release metadata, requests elevation, or writes
outside its installation location and a private temporary directory.

## Architecture

- Add an `updates` domain under `packages/conduit/src/domains/updates` owning
  update commands, queries, handlers, repository/service interfaces, value
  types, enums, and errors.
- Release discovery and installation implementations live in the updates
  domain behind typed interfaces. Generic HTTP and child-process primitives may
  live in `src/system`; platform-neutral pure SemVer and checksum helpers may
  live in `src/helpers/<category>`.
- Register update handlers through an `UpdatesBootstrapService`. Do not add
  update behavior directly to the application composition root.
- The TUI renders update state and delegates through the command/query buses.
  `HomeScreen`, controllers, and `UpdateScreen` do not access the network,
  package managers, release files, or the current executable directly.
- The running version has one generated build-time source of truth shared by
  package and standalone builds; duplicated hand-maintained literals are not
  accepted.
- All new TypeScript follows strict domain ownership and NodeNext `.js` import
  conventions. Shared theme tokens are used for every update state.

## Security and integrity

- Only HTTPS release metadata and assets from the allowlisted official GitHub
  release hosts are accepted. Redirects cannot escape the allowlist.
- Standalone assets must match the checksum entry for the exact expected asset
  name before any replacement. A missing, duplicate, malformed, or mismatched
  checksum aborts the update.
- Release tags, asset names, response text, paths, and child-process output are
  untrusted inputs. They are validated, bounded, and never interpolated into a
  shell.
- Package-manager execution uses an executable plus argument array with shell
  mode disabled.
- Logs and UI diagnostics are sanitized and contain no environment secrets,
  authorization headers, project data, or arbitrary response bodies.

## Acceptance criteria

- [ ] Every interactive bare `conduit` bootstrap starts exactly one
      non-blocking stable-release check and renders Home without waiting for it.
- [ ] Home always shows the current version and clearly distinguishes checking,
      current, update-available, and unavailable states.
- [ ] `u` opens an explicit, default-cancel confirmation only when Home owns
      focus and an automatic or guided update is available.
- [ ] Confirmation opens an update screen with loader, typed progress stages,
      source/target versions, and terminal success or failure behavior.
- [ ] Official standalone updates verify the target asset checksum and replace
      safely without destroying the running installation on failure.
- [ ] Supported global package installs update only
      `conduit-orchestrator@<exact-version>`; local/unknown installs receive
      non-mutating manual guidance.
- [ ] Offline, timeout, rate-limit, invalid metadata, unsupported platform, and
      installation failures do not block bootstrap or project work.
- [ ] Non-interactive commands never perform update network I/O or emit update
      notices.
- [ ] No project data, secrets, telemetry, or stable machine identifier leaves
      the process during update discovery or installation.

## Non-goals

- Background daemons, scheduled polling, forced updates, silent installation,
  release channels, prerelease opt-in, downgrade, or rollback to arbitrary
  versions.
- Updating coding-agent providers, project dependencies, plugins, skills,
  feature packets, or project configuration.
- Changing GitHub release publication, introducing a new release host, or
  treating checksums as cryptographic publisher signatures.
- Persisting update state in the project or global database.

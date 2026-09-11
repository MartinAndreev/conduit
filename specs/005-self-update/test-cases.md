# QA test cases

## Verification status

Focused update tests shipped with v0.6.0 and passed in the September 2026 local
run. Cases that require a real release artifact, another operating system, a
live Pages/release environment, or broader interaction coverage remain
unchecked. The repository-wide CLI suite currently has 21 pre-existing
worktree/recovery failures, so the regression gate is not marked complete.

## Bootstrap and discovery

- [x] Home renders before a deliberately delayed release response completes.
- [x] One interactive bootstrap creates exactly one request even when multiple
      components subscribe to update state.
- [x] `--help`, `--version`, `version`, compact commands, piped output, and CI
      perform no release request and emit no update message.
- [x] Valid newer stable SemVer is selected; equal, older, draft, prerelease,
      malformed, and lexicographically misleading tags are ignored.
- [x] Timeout, DNS/offline failure, HTTP error, rate limit, oversized response,
      invalid JSON, and disallowed redirect leave Home usable.
- [x] Discovery sends only the required HTTP metadata and no project path,
      feature content, credential, environment secret, or machine identifier.

## Home and confirmation

- [x] Current version remains visible while checking and in every terminal
      discovery state.
- [x] An available update shows its target version and `[u] Update`; an
      up-to-date result does not claim an update is available.
- [x] `u` opens confirmation from idle Home but not while search, feature
      creation, an action modal, or another screen owns focus.
- [x] Confirmation displays source/target versions and detected method, defaults
      to cancel, and Escape/`q` returns without installation work.
- [ ] Narrow terminals preserve a readable version/update state and actionable
      confirmation rather than overflowing or hiding the cancel path.

## Update screen

- [x] Confirmation transitions through applicable typed `preparing`,
      `downloading`, `verifying`, `installing`, and `complete` states.
- [x] Loader animation stops in success, failure, and cancellation states.
- [x] Success tells the user that the running process is still the old version
      and provides the approved restart/exit action.
- [ ] Each failure is sanitized, retains a usable old installation, and exposes
      retry or return-to-Home without trapping keyboard focus.

## Standalone integrity and recovery

- [x] The expected platform asset and checksum file are fetched only from the
      selected allowlisted release.
- [x] Missing, duplicate, malformed, mismatched, or wrong-asset checksums abort
      before executable replacement.
- [ ] Interrupted download, full disk, read-only destination, replacement
      failure, and Windows locked-executable behavior preserve the old binary
      and clean private temporary artifacts where safe.
- [ ] A successful replacement is executable, reports the target version on
      next launch, and cannot expose a partially written destination.
- [x] Automatic standalone replacement is disabled for any platform lacking a
      passing release-matrix integration test.

## Package-managed and unsupported installs

- [x] A supported global install invokes only its detected package-manager
      executable with an argument array and
      `conduit-orchestrator@<exact-target-version>`.
- [x] Package-manager failure captures bounded sanitized diagnostics and leaves
      project manifests and lockfiles unchanged.
- [x] Local dependency, source checkout, unknown manager, unsupported platform,
      and unwritable destination perform no mutation and show accurate manual
      guidance.
- [ ] No path invokes a shell, requests elevation, changes project config, or
      updates unrelated dependencies.

## Regression gates

- [ ] Formatting, lint, strict typecheck, tests, package build, and
      `pnpm start --help` pass.
- [ ] Home feature navigation, search, creation, action modal, refinement, run,
      status, and quit key behavior remain unchanged outside update focus.
- [ ] Supported standalone release artifacts pass next-launch update tests
      without `node_modules`.

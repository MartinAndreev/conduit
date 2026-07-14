# QA test cases

## Bootstrap and discovery

- [ ] Home renders before a deliberately delayed release response completes.
- [ ] One interactive bootstrap creates exactly one request even when multiple
      components subscribe to update state.
- [ ] `--help`, `--version`, `version`, compact commands, piped output, and CI
      perform no release request and emit no update message.
- [ ] Valid newer stable SemVer is selected; equal, older, draft, prerelease,
      malformed, and lexicographically misleading tags are ignored.
- [ ] Timeout, DNS/offline failure, HTTP error, rate limit, oversized response,
      invalid JSON, and disallowed redirect leave Home usable.
- [ ] Discovery sends only the required HTTP metadata and no project path,
      feature content, credential, environment secret, or machine identifier.

## Home and confirmation

- [ ] Current version remains visible while checking and in every terminal
      discovery state.
- [ ] An available update shows its target version and `[u] Update`; an
      up-to-date result does not claim an update is available.
- [ ] `u` opens confirmation from idle Home but not while search, feature
      creation, an action modal, or another screen owns focus.
- [ ] Confirmation displays source/target versions and detected method, defaults
      to cancel, and Escape/`q` returns without installation work.
- [ ] Narrow terminals preserve a readable version/update state and actionable
      confirmation rather than overflowing or hiding the cancel path.

## Update screen

- [ ] Confirmation transitions through applicable typed `preparing`,
      `downloading`, `verifying`, `installing`, and `complete` states.
- [ ] Loader animation stops in success, failure, and cancellation states.
- [ ] Success tells the user that the running process is still the old version
      and provides the approved restart/exit action.
- [ ] Each failure is sanitized, retains a usable old installation, and exposes
      retry or return-to-Home without trapping keyboard focus.

## Standalone integrity and recovery

- [ ] The expected platform asset and checksum file are fetched only from the
      selected allowlisted release.
- [ ] Missing, duplicate, malformed, mismatched, or wrong-asset checksums abort
      before executable replacement.
- [ ] Interrupted download, full disk, read-only destination, replacement
      failure, and Windows locked-executable behavior preserve the old binary
      and clean private temporary artifacts where safe.
- [ ] A successful replacement is executable, reports the target version on
      next launch, and cannot expose a partially written destination.
- [ ] Automatic standalone replacement is disabled for any platform lacking a
      passing release-matrix integration test.

## Package-managed and unsupported installs

- [ ] A supported global install invokes only its detected package-manager
      executable with an argument array and
      `conduit-orchestrator@<exact-target-version>`.
- [ ] Package-manager failure captures bounded sanitized diagnostics and leaves
      project manifests and lockfiles unchanged.
- [ ] Local dependency, source checkout, unknown manager, unsupported platform,
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

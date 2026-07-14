# Plan: Bootstrap update check and guided self-update

## Delivery sequence

### 005-A Contracts and deterministic discovery

- Define domain-owned update state, release, installation, progress, command,
  query, and error contracts.
- Generate the running version from package metadata for package and standalone
  builds.
- Implement stable SemVer selection, bounded GitHub release discovery, request
  coalescing, timeout behavior, and sanitized failure mapping.
- Register update handlers through an updates bootstrap service.

### 005-B Home version status and confirmation

- Start discovery once during interactive application bootstrap without
  awaiting it before Home renders.
- Add current/checking/current/available/unavailable read-model rendering to
  Home using theme tokens.
- Add focus-safe `u` handling and a default-cancel confirmation dialog.
- Keep all non-interactive command paths free of update checks and notices.

### 005-C Installation strategies and update screen

- Detect official standalone, supported global package-manager, and
  non-self-updatable installations.
- Implement verified staged standalone replacement and exact-version global
  package update behind domain interfaces.
- Implement typed progress publication and the update screen loader, terminal
  states, retry, return, and restart guidance.
- Add cleanup and previous-install preservation for every failure boundary.

### 005-D Release-quality verification

- Add unit, integration, TUI interaction, process, and platform-strategy tests
  from `test-cases.md` using local fake release servers and temporary installs.
- Verify network payload privacy, shell-free execution, checksum enforcement,
  bounded diagnostics, focus ownership, and non-interactive silence.
- Update README/TUI/release documentation and exercise package plus supported
  standalone targets.

## Delivery gate

Each group requires approved packet scope, implementation, review, correction
if needed, acceptance, and commit before the next group begins. Do not combine
the updater with Feature 002 or 003 work or change their contracts.

## Verification commands

- `pnpm format:check`
- `pnpm lint`
- `pnpm typecheck`
- `pnpm test`
- `pnpm build`
- `pnpm start --help`
- `pnpm build:standalone -- linux-x64`
- Standalone updater integration jobs for every supported release target before
  enabling automatic replacement on that target.

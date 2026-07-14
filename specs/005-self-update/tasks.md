# Tasks

## 005-A Contracts and deterministic discovery

- [ ] Define update domain types, enums, errors, commands, queries, progress
      events, and release/install service interfaces.
- [ ] Replace hand-maintained version literals with one generated build-time
      version source for package and standalone outputs.
- [ ] Implement stable SemVer release selection and bounded official GitHub
      release discovery with timeout, allowlisted redirects, and coalescing.
- [ ] Register update commands and queries through `UpdatesBootstrapService`.

## 005-B Home version status and confirmation

- [ ] Trigger one background check per interactive application bootstrap after
      required local bootstrap succeeds.
- [ ] Render the current version and update-check state in Home using theme
      tokens.
- [ ] Add focus-safe `[u] Update` behavior and a default-cancel confirmation
      naming current version, target version, and installation strategy.
- [ ] Prove scriptable and non-interactive paths make no update request and
      print no update notice.

## 005-C Installation strategies and update screen

- [ ] Implement fail-closed installation detection for official standalone,
      supported global package-manager, local/source, and unknown installs.
- [ ] Implement exact-asset download, checksum validation, staging, platform-safe
      replacement, cleanup, and preservation of the old standalone executable.
- [ ] Implement shell-free exact-version global package update without changing
      project manifests or lockfiles.
- [ ] Implement `UpdateScreen` with loader, typed stages, sanitized errors,
      retry/return actions, and restart guidance.

## 005-D Release-quality verification

- [ ] Add fake-server discovery, malformed input, timeout, privacy, checksum,
      process invocation, cleanup, and replacement integration tests.
- [ ] Add Home/update-screen keyboard, focus, resize, success, failure, cancel,
      and offline interaction tests.
- [ ] Verify package build, `pnpm start --help`, Linux x64 standalone, and the
      platform release matrix before enabling each automatic strategy.
- [ ] Document version status, update controls, supported strategies, manual
      fallback, integrity model, and offline behavior.

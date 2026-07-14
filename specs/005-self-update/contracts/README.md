# Contracts

Feature 005 must define these contracts during implementation:

- `ReleaseDiscovery` for bounded stable-release lookup.
- `InstallationDetector` for fail-closed standalone, global-package, local,
  source, unsupported, and unknown classification.
- `UpdateInstaller` strategies for verified standalone replacement and
  shell-free exact-version package updates.
- A versioned update status read model covering idle, checking, current,
  available, unavailable, updating, succeeded, and failed states.
- Typed update progress events for preparing, downloading, verifying,
  installing, and completion.
- A check-for-update query and start-update command in the updates domain's
  `interfaces/queries` and `interfaces/commands` folders.
- Domain errors for discovery, validation, integrity, permission, platform,
  process, replacement, and recovery failures with sanitized user-facing
  mappings.

Implementations must preserve CQRS and repository ownership: TUI code only
renders read models and dispatches actions; network, filesystem replacement,
and process execution stay behind update-domain interfaces. Shared interfaces,
enums, and value types must not be declared in handlers, screens, controllers,
or a global catch-all module.

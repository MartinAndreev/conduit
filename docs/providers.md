# Providers and credentials

## Provider model

The first functional provider is Local Spec Kit. It discovers approved feature packets beneath the configured `specsDir`, reads metadata and packet files, and never requires credentials.

Future GitHub, Linear, Jira, and Asana providers implement the same provider contract rather than changing Home or feature screens. They are not selectable in the first release.

## Settings and credentials

Global settings use the platform configuration location. Project `conduit.yml` overrides non-secret settings and selects a named global credential profile. Precedence is CLI option, project config, global config, built-in default.

Secrets are stored in the operating-system credential vault. If that service is unavailable, Conduit writes an encrypted fallback vault in the global configuration directory using a random locally generated key. Project configuration contains credential profile identifiers only; plaintext tokens are prohibited everywhere outside the credential store.

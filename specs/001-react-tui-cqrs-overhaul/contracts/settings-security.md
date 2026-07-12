# Settings and security contract

Global settings hold provider metadata and named credential-profile references. Project `conduit.yml` may override non-secret settings and select a named global profile. Credentials are fetched only through `CredentialStore`.

The store uses the OS credential vault first. Its encrypted fallback is global-only. Plaintext token values are prohibited in repository files, project state, feature packets, prompts, logs, command arguments, test fixtures, and UI read models.

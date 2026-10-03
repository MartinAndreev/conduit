# Startup adapters

Both probes invoke only `--version`, with no shell or prompt. Primary evidence:
- Codex's Clap CLI declares `version`: https://github.com/openai/codex/blob/main/codex-rs/cli/src/main.rs
- OpenCode documents the version flag: https://opencode.ai/docs/cli/

Fixtures accept `codex-cli 0.159.3` and OpenCode `1.2.3`; unknown output remains unknown and is never exposed. Presence/version does not establish supported session protocol, authentication, or model availability. Production limits: 3 seconds and 16 KiB shared stdout/stderr capture. Both pipes continue draining after capture fills. Adapter-owned probe descriptors select the executable, arguments, and parser; configuration owns the supported schema IDs. New descriptors do not silently broaden schema v1.

Each started probe has a cleanup owner in `internal/process`. Cleanup attempts tree termination on both root exit and cancellation, closes owned pipes, and retains responsibility for eventual reap. The caller waits at most a separate 250ms cleanup budget; timeout returns an unconfirmed result with an application observer in `Outcome.PendingCleanup`. This stops further startup effects and preserves the technical cause. Repeated cleanup requests on the same owner do not create new workers. Root reap, tree termination result, pipe closure, and native resource release are distinct facts. Successful signal delivery is not proof of immediate descendant quiescence; if the OS refuses termination, the outcome does not promise it. The observer and cleanup owner live within the running application, not a detached service. Final application exit behavior with unresolved cleanup belongs to the presentation/lifetime stage.

Windows starts the child suspended, assigns it to a kill-on-close Job Object, then resumes the initial thread. Nested-job assignment failure rejects launch while the root is still suspended. Native Windows verification remains required.

`Files.Read` composes a bounded file query, YAML decoding, and public result mapping. The query preserves native `Stat` errors separately from identity changes, rejects non-regular targets, and distinguishes an initially absent file from one that disappears after lookup. The shared mutex wrapper is `internal/lockscope`; capture, progress, and process state use it without moving I/O into their state critical sections.

Publication prepares a private temp in the target directory using `os.CreateTemp` with mode 0600 before the Unix umask; a stricter umask remains respected. Preparation writes, syncs, and closes the whole file before `Publish` uses `os.Link` to publish without replacement. Write, short-write, sync, close, and cleanup errors remain observable. A filesystem lacking hard links returns an error; there is no unsafe fallback. AlreadyExists includes existing symlinks, which Read rejects. Unix syncs the directory after publication; an error at this point returns Published plus an error. Windows syncs file contents but does not claim directory-entry durability through power loss. Atomic visibility and no overwrite are guaranteed by the link primitive; full power-loss durability is not promised. Cleanup removes only this operation's temporary file. The user home is trusted; this is not a security boundary against a hostile process replacing ancestor directories concurrently.

Windows ownership follows Microsoft's Job Objects and ResumeThread contracts:
- https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects
- https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-resumethread

The job permits neither explicit nor silent breakaway. Ordinary CreateProcess descendants inherit membership; external service/WMI launches are not a process-tree ownership guarantee. Suspended launch prevents the initial process from running before assignment. Native Windows runtime/nested-job behavior must be verified separately; Windows amd64/arm64 cross-compilation alone does not prove it.

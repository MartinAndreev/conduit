# Runner event contract

All runners emit a normalized discriminated event: lifecycle, activity, tool-call, tool-output, file-change, patch, error, or result. Each event includes run id, role id, timestamp, and safe display payload. Raw runner output remains a local captured artifact and is never treated as an authoritative diff.

Codex uses JSONL, OpenCode uses JSON output, Pi uses JSON mode, and Kilo uses JSON output. An executable missing from PATH produces an `unavailable` lifecycle/error state rather than a spawn exception reaching a screen.

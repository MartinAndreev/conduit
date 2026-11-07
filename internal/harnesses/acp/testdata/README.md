# ACP protocol fixtures

Reduced, sanitized JSON-RPC transcripts rewritten by hand from the shapes in
the T3 Code ACP recordings (MIT; fx 0.0.13/0.0.14 and the generic
`acp_elicitation` scenario). No frame is copied verbatim. Session, message
and tool-call IDs are synthetic, paths are relative placeholders and prompts
are neutral. Lines starting with `#` are comments.

Each line is one step: `from:"client"` is a frame the adapter must write
(`match` is a subset of its params or result); `from:"agent"` is a frame the
fake agent writes. An agent `response` reuses the ID of the latest client
request with the same method.

| File | Scenario |
| --- | --- |
| `turn_end_turn.ndjson` | initialize, session/new, thought/text chunks, edit and generic tool calls, usage, `end_turn`, session/close |
| `turn_cancelled.ndjson` | session/cancel answered by `stopReason:"cancelled"` |
| `permission_reject_once.ndjson` | `session/request_permission` answered `reject_once`, failed edit tool |
| `elicitation_decline.ndjson` | `elicitation/create` declined |
| `prompt_error.ndjson` | JSON-RPC error on session/prompt |
| `session_settings.ndjson` | descriptor-required `mode=auto` applied with session/set_config_option |

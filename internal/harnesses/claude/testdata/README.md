# Claude Code stream-json fixtures

Synthesized, sanitized fixtures for the Claude harness adapter tests. They
reproduce only message shapes; they contain no user paths, skills, account data
or model output from a real session.

- `initialize_*.json`: the `control_response.response.response` body of the
  `initialize` control request, shaped after a live `claude 2.1.316` recording
  (2026-11-07, no login). `initialize_ok.json` differs only in `tokenSource`.
- `turn_api_error.ndjson`: one turn as recorded live without a login
  (command_lifecycle, system/init, system/status, replay echo, `<synthetic>`
  assistant, `result{subtype:"success",is_error:true,terminal_reason:"api_error"}`).
- `turn_tools.ndjson`, `steer_*.ndjson`, `wake_result.ndjson`: shapes not present
  in the live recording (stream_event deltas, tool_use/tool_result,
  structured_output, aborted_streaming, task-notification). They are written
  after the shapes of public MIT-licensed transcript fixtures and remain
  fixture-only evidence.

Placeholders replaced by the tests: `{{SESSION}}` (native session UUID),
`{{INPUT}}` (the turn's message UUID), `{{STEER}}` (the steer message UUID).

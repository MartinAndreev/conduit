# Plan

1. Add JSON Schemas for `AgentAssignmentV1`, `AgentResponseV1`, and `ConduitResultRecordV1`.
2. Add TypeScript protocol types and strict structural validation kept separate from semantic assignment validation.
3. Make system runner adapters the single source for commands, final capture, native capabilities, and incremental event parsing.
4. Wire incremental transport parsing into process execution while preserving raw sanitized logs and Git/worktree file-change observation.
5. Require validated final responses for implementation, QA, documentation, research, architect, reviewer, and custom roles before dependency success.
6. Render research and clarification Markdown from validated universal responses.
7. Add coverage for parser chunking, validation failures, role policies, runner fixtures, and completion semantics.

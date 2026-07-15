# Decisions

- One universal final response, `AgentResponseV1`, is used for all current and future roles.
- Role-specific requirements are semantic policy, not separate wire formats.
- Agent-reported artifacts and verification are claims; Git/worktree inspection and Conduit-observed events remain authoritative.
- Native runner schema enforcement is opportunistic. Conduit validation is mandatory for every runner.
- Markdown fences or surrounding prose are invalid protocol output.
- Owned paths define planning and integration responsibility. Unexpected writable paths are preserved as structured ownership warnings for review; they do not fail an otherwise valid response. Read-only writes and explicitly forbidden-path changes remain hard semantic failures.

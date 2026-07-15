# Decisions

- One universal final response, `AgentResponseV1`, is used for all current and future roles.
- Role-specific requirements are semantic policy, not separate wire formats.
- Agent-reported artifacts and verification are claims; Git/worktree inspection and Conduit-observed events remain authoritative.
- Native runner schema enforcement is opportunistic. Conduit validation is mandatory for every runner.
- Markdown fences or surrounding prose are invalid protocol output.

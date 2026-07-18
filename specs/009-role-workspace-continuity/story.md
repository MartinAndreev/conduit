# Feature 009 Story: Role Workspace Continuity

As a Conduit operator, I want each configured role to have one recoverable workspace slot per repository so that interrupted work can continue after a crash without accumulating run-nested worktrees, while completed workflows clean up all temporary Git state.

When compatible retained work exists, Conduit should explain what can be continued and let me choose between continuing it or explicitly starting anew. It must never mix work from different repository revisions, feature packages, assignments, or concurrent runs merely because the role name matches.

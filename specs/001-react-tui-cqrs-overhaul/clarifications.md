# Architecture decisions

- Use React bindings for OpenTUI and migrate the application to strict TypeScript/TSX.
- Keep the CLI command surface. Bare `conduit` opens Home; interactive `refine` opens the shared React refinement screen; operational commands remain compact by default.
- Local Spec Kit is the only functional provider in this release. Remote providers are future adapters.
- Use platform-global settings with project-local non-secret overrides. Project configuration selects a named global credential profile.
- Store secrets in the OS credential vault with an encrypted global-vault fallback when unavailable.
- Use Tab/Shift+Tab for field navigation and Ctrl+Enter to submit refinement forms.
- Persist rejected/quit drafts in ignored local state for Resume or Discard.
- Use built-in FrameBuffer role portraits with project-configured asset-path overrides.
- Use the logo palette as TUI design tokens; add one accessible semantic error-red token for error/not-started states.
- Implement all task groups sequentially: OpenCode implementation, Codex architect review, then a commit.

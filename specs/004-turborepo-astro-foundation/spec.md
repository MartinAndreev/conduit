# Turborepo and Astro foundation

## Outcome

Conduit is a pnpm/Turborepo workspace with separately deployable CLI and
website packages. The CLI retains its public command and release behaviour;
the Astro website publishes static content to GitHub Pages.

## Acceptance criteria

- [ ] `packages/conduit` remains publishable as `conduit-orchestrator` and
      retains the `conduit` binary.
- [ ] Root developer commands run the workspace build, lint, typecheck, and
      test tasks.
- [ ] `packages/website` builds as a static Astro application.
- [ ] GitHub Pages deployment is separate from the standalone-binary release
      workflow and deploys only the website artifact.
- [ ] CLI project resolution remains based on the invocation directory or
      explicit `--project`, never the package directory.
- [ ] Features 002 and 003 remain unimplemented and their contracts unchanged.

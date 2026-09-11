# Turborepo and Astro foundation

## Delivery status

Implemented in `e92c939`. The workspace, publishable CLI package, static Astro
site, and separate Pages workflow remain present. The original sequencing
constraint about Features 002 and 003 was satisfied when this packet shipped
and is historical now; persistence subsequently shipped and future memory work
belongs to a Feature 007 extension.

## Outcome

Conduit is a pnpm/Turborepo workspace with separately deployable CLI and
website packages. The CLI retains its public command and release behaviour;
the Astro website publishes static content to GitHub Pages.

## Acceptance criteria

- [x] `packages/conduit` remains publishable as `conduit-orchestrator` and
      retains the `conduit` binary.
- [x] Root developer commands run the workspace build, lint, typecheck, and
      test tasks.
- [x] `packages/website` builds as a static Astro application.
- [x] GitHub Pages deployment is separate from the standalone-binary release
      workflow and deploys only the website artifact.
- [x] CLI project resolution remains based on the invocation directory or
      explicit `--project`, never the package directory.

## Historical delivery constraint

- [x] During Feature 004 delivery, Features 002 and 003 remained unimplemented
      and their contracts were not changed by the workspace relocation.

That constraint does not describe current repository state. Feature 002 later
shipped in `ea0d012`; Feature 003's completed scope is historical, and new
memory lifecycle/retrieval work is assigned to a Feature 007 extension.

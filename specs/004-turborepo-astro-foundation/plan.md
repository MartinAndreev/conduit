# Plan: Turborepo and Astro foundation

## Purpose

Establish a pnpm workspace and Turborepo task graph before Feature 002 (local
Turso persistence) and Feature 003 (repository memory). Preserve Conduit's
current CLI behaviour and release outputs while creating an independent Astro
website application.

This is a repository-structure change only. It does not implement any Feature
002 or 003 storage, profile, memory, indexing, or handoff behaviour.

## Decisions to approve before implementation

1. Use pnpm workspaces with Turborepo; pnpm remains the sole package manager.
2. Move the publishable CLI to `packages/conduit`, retaining the package name,
   `conduit` binary name, versioning, and package contents expected by users.
3. Create the Astro site as the `packages/website` workspace package. It is a
   separately deployable package and
   must not be bundled into the CLI or its standalone targets.
4. Publish the static website to GitHub Pages through a dedicated GitHub Actions
   workflow on pushes to `main` that affect the site or its shared workspace
   configuration. The workflow is independent of tagged CLI binary releases.
5. Treat the Git repository root as the Conduit project root when Conduit is
   run from this repository. The CLI package directory is never a target
   project's root.
6. Keep root-level repository documents and feature packets in place:
   `AGENTS.md`, `README.md`, `docs/`, `specs/`, and shared repository config.

## Work sequence

### 1. Establish the workspace without moving application code

- Add `pnpm-workspace.yaml` covering `packages/*`.
- Add `turbo.json` with explicit `build`, `lint`, `typecheck`, `test`, and
  development task dependencies and output declarations.
- Convert the root manifest to a private workspace coordinator. Root commands
  delegate to the Conduit package, preserving the familiar `pnpm build`,
  `pnpm test`, and `pnpm start --help` developer experience.
- Define cache inputs so changes to role templates, source, build scripts, and
  package manifests invalidate the appropriate tasks. Do not cache commands
  that write release artifacts or depend on local project state.

### 2. Relocate the CLI as a publishable workspace package

- Move `src/`, `bin/`, `skills/`, CLI build scripts, tests, and package-specific
  tooling into `packages/conduit` using Git-aware moves.
- Update package-relative paths in build scripts, tests, template generation,
  and packaging metadata.
- Preserve NodeNext ESM import conventions and the existing Bun build and
  standalone build targets.
- Confirm CLI project resolution continues to use the invocation directory or
  explicit `--project`, rather than paths relative to `packages/conduit`.

### 3. Add a minimal Astro website package

- Scaffold `packages/website` with Astro, TypeScript, site-local scripts, and a
  minimal static landing page.
- Keep the app isolated from CLI source imports. Shared types or components are
  out of scope unless a later approved feature establishes a stable public
  package boundary.
- Add only the deployment-neutral configuration required to build and preview
  the site locally; hosting, analytics, CMS, and authentication are deferred.

### 4. Make repository tooling workspace-aware

- Adjust ESLint, Prettier, TypeScript, Husky/lint-staged, and release scripts
  so root commands cover both workspace packages without weakening strict
  TypeScript checks.
- Ensure generated and release outputs remain ignored and that the published
  CLI package includes its role assets and generated artifacts exactly as
  before.
- Update README contributor instructions with root and package-level commands.

### 5. Add GitHub Pages deployment

- Add a dedicated `.github/workflows/deploy-website.yml` workflow, triggered by
  pushes to `main` that affect `packages/website`, workspace manifests and lock
  files, or the deployment workflow itself; include `workflow_dispatch` for a
  manual redeploy.
- Give this workflow only the permissions GitHub Pages requires, build the
  website with a frozen lockfile, upload its static output as the Pages
  artifact, and deploy that artifact through the official Pages deployment
  action.
- Configure Astro's production base path from the GitHub Pages configuration so
  project pages work without hard-coding this repository's owner or name.
- Use a deployment concurrency group so an older website build cannot publish
  after a newer one. Keep the existing tag-triggered standalone-binary release
  workflow unchanged.
- Document that the repository's GitHub Pages source must be set to GitHub
  Actions before the first deployment. This is a repository setting that cannot
  be changed by the workflow alone.

### 6. Verify compatibility and establish the baseline

- Run workspace installation with the lockfile updated once for the new
  dependency graph.
- Run root lint, formatting check, typecheck, and tests.
- Run `pnpm start --help` from the repository root and execute a representative
  CLI command using both the current directory and `--project` against a
  temporary target repository.
- Build the CLI package and one standalone target; verify the generated binary
  still starts and resolves its bundled role templates.
- Build the Astro application and verify its production preview serves the
  landing page.
- Validate the Pages workflow on a branch or manual dispatch: it must build,
  upload, and deploy the static artifact without running CLI release steps.

## Explicit non-goals

- No Turso dependency, database schema, migration, profile, or configuration
  resolution work from Feature 002.
- No indexing, observations, memory storage, model calls, or handoff work from
  Feature 003.
- No shared design system, API service, authentication, documentation CMS,
  non-GitHub-Pages deployment provider, telemetry, or CLI release automation
  redesign.
- No change to Conduit's public command syntax, target-project file layout, or
  current release artifact names.

## Risks and acceptance gates

| Risk                                                                             | Gate                                                                                                                   |
| -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Workspace relocation makes the package directory appear to be the target project | Integration tests cover invocation from root, package directory, and explicit external `--project`.                    |
| Turbo caching hides stale role templates or build artifacts                      | Build tests change a role/template input and assert regeneration before packaging.                                     |
| CLI publication or standalone builds lose assets                                 | Package-content and standalone smoke tests verify the binary and bundled roles.                                        |
| Astro dependencies destabilize CLI tooling                                       | CLI checks run independently and site dependencies remain scoped to `packages/website`.                                |
| A stale or misbased site build is published                                      | The Pages workflow uses deployment concurrency and a production build that receives the Pages base-path configuration. |

## Follow-on order

After this packet is accepted and implemented, begin Feature 002 against the
settled workspace layout. Feature 003 follows Feature 002, because its memory
repositories depend on the persistence foundation.

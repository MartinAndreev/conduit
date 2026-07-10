<p align="center">
  <img src="assets/conduit-logo.svg" alt="Conduit" width="720" />
</p>

<p align="center"><strong>Spec-driven orchestration for coding-agent teams.</strong></p>

Conduit turns an approved feature specification into bounded, parallel agent work. It keeps the decisions that matter in Git, gives each worker a clear ownership boundary, and makes the noisy parts of agent execution inspectable without overwhelming the terminal.

## What it does

```text
Refine story → approve spec and contracts → parallel workers → QA/review → integrate
```

- Creates committed feature packets: `story.md`, `spec.md`, `plan.md`, `tasks.md`, contracts, and QA cases.
- Runs Codex for high-value requirement and architecture refinement.
- Delegates bounded work to Codex, OpenCode, Pi, or Kilo.
- Isolates writing workers in Git worktrees.
- Captures agent transcripts locally and presents compact OpenTUI views with expandable logs and diffs.
- Keeps run state, caches, and worktrees out of Git.

## Install

### Standalone binary — recommended

Download a platform binary from GitHub Releases, verify it against `SHA256SUMS`, and place it on `PATH`:

```bash
chmod +x conduit-linux-x64
install -m 755 conduit-linux-x64 ~/.local/bin/conduit
```

Release assets also include installers:

```bash
curl -fsSL https://github.com/MartinAndreev/conduit/releases/latest/download/install.sh | sh
```

```powershell
irm https://github.com/MartinAndreev/conduit/releases/latest/download/install.ps1 | iex
```

### Development package

```bash
pnpm add -D conduit-orchestrator
pnpm exec conduit init .
```

## Quick start

Initialize an existing Git repository:

```bash
conduit init .
```

Create a feature packet, capture the story, then have the architect refine it:

```bash
conduit feature "Add team invitations"
conduit refine 001
conduit refine 001 --architect
```

The interactive refinement view accepts multiline input. Press `Ctrl+Enter` to save a field. Architect refinement opens a live, compact dashboard in a compatible terminal; use `--compact` for a one-line progress mode.

When a material product or technical decision is unclear, the architect stops instead of assuming. Conduit shows its questions in a multiline answer field, stores your response in `clarifications.md`, and resumes refinement. `questions.md` is only a temporary handoff file; `clarifications.md` should be committed with the feature packet.

Plan implementation before launching any agent:

```bash
conduit run 001 --roles frontend,backend --dry-run
```

Then execute the isolated workers:

```bash
conduit run 001 --roles frontend,backend --execute
conduit status --tui
```

## Feature packet

```text
specs/001-team-invitations/
  story.md          # refined product intent
  spec.md           # approved behavior and acceptance criteria
  plan.md           # architecture and ownership
  tasks.md          # bounded implementation work
  test-cases.md     # QA scenarios and regressions
  clarifications.md # answered architect questions and decisions
  contracts/        # API, event, type, and UI boundaries
```

These files are intended to be committed. Local execution artifacts are ignored automatically:

```gitignore
.conduit/runs/
.conduit/cache/
.conduit/worktrees/
```

## Roles and runners

Roles live in `conduit.yml`; their prompts are editable under `.conduit/roles/`. `conduit roles` prints the configured role, runner, purpose, and skill source.

Built-in roles are intentionally narrow: `architect` produces the specification and contracts; `researcher` gathers repository evidence without edits; `frontend` and `backend` implement separate owned paths; `qa` writes tests; `documentation` owns user, operator, and developer documentation; and `reviewer` independently checks the integrated result. Each role skill defines its required inputs, boundaries, and reporting format, and can be replaced per project.

```yaml
roles:
  frontend:
    runner: opencode
    mode: subagent
    model: openai/gpt-5-mini
    owns: [apps/web, packages/ui]
    skill:
      source: file:.conduit/roles/frontend.md

  qa:
    runner: pi
    mode: subagent
    owns: [tests, e2e]
    skill:
      source: file:.conduit/roles/qa.md

  documentation:
    runner: opencode
    mode: subagent
    owns: [docs, README.md]
    skill:
      source: file:.conduit/roles/documentation.md
```

Supported runners are `codex`, `opencode`, `pi`, and `kilo`. A role can use a built-in skill, a project-local file, or a SHA-256-pinned HTTPS skill file.

`model` is optional. When provided, Conduit passes it to the selected runner (for example, `provider/model` for OpenCode and Kilo, or a Pi model pattern). Omit it to use that runner's configured default model.

### Model examples by provider

Use a model ID supported by the runner you choose:

```yaml
roles:
  # Codex uses the model access available to the signed-in ChatGPT/OpenAI account.
  architect:
    runner: codex
    model: gpt-5.6-terra

  # OpenCode Go model IDs use the opencode-go/<model-id> provider prefix.
  researcher:
    runner: opencode
    model: opencode-go/mimo-v2.5-pro

  # OpenCode also supports the providers configured in opencode.json.
  frontend:
    runner: opencode
    model: openai/gpt-5-mini

  # Pi accepts provider/model IDs or its documented model patterns.
  qa:
    runner: pi
    model: openai/gpt-5-mini

  # Kilo uses provider/model IDs from its configured providers.
  backend:
    runner: kilo
    model: anthropic/claude-sonnet-4-5
```

Use `opencode models`, `pi --list-models`, or `kilo models` to discover models available through your authenticated provider configuration. For OpenCode Go, MiMo-V2.5-Pro is `opencode-go/mimo-v2.5-pro`.

## Dashboard

`conduit status --tui` keeps agent activity concise:

```text
› ✓ architect     codex
    • Ran project discovery
      └ 63 lines captured · Enter to preview patch
```

Raw transcripts are captured to `.conduit/runs/`, while patches expand with a native diff view and file navigation.

## Development

Conduit uses Bun as its single build tool. Install Bun and ensure `bun` is on your `PATH` when developing or publishing Conduit; end users of the npm package and standalone binary do not need Bun at runtime.

```bash
curl -fsSL https://bun.sh/install | bash
pnpm install
pnpm test
pnpm build
```

Build a self-contained Linux binary with Bun:

```bash
pnpm build:standalone -- linux-x64
dist/release/conduit-linux-x64 --version
```

## License

MIT. See [LICENSE](LICENSE).

Install that local binary onto your `PATH` with:

```bash
pnpm install:local
```

The release workflow builds Linux, macOS, and Windows binaries when a version tag is pushed.

## License

Choose and add a license before publishing the first public release.

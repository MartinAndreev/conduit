<p align="center">
  <img src="assets/conduit-logo.svg" alt="Conduit" width="720" />
</p>

<p align="center"><strong>Spec-driven orchestration for coding-agent teams.</strong></p>

Conduit turns an approved feature specification into bounded, parallel agent work. It keeps the decisions that matter in Git, gives each worker a clear ownership boundary, and makes the noisy parts of agent execution inspectable without overwhelming the terminal.

## What it does

```text
Refine story → approve spec and contracts → parallel workers → QA/review → integrate
```

- Opens a React/OpenTUI workspace from bare `conduit` for the day-to-day flow: create features, refine packets, launch runs, and inspect status.
- Creates committed feature packets: `story.md`, `spec.md`, `plan.md`, `tasks.md`, contracts, QA cases, and clarification history.
- Runs Codex for high-value requirement and architecture refinement from the TUI or a compact command-line fallback.
- Delegates bounded work to Codex, OpenCode, Pi, or Kilo.
- Isolates writing workers in Git worktrees.
- Captures agent transcripts locally and presents compact TUI views with expandable logs, review findings, and split diffs.
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

Then run Conduit with no arguments:

```bash
conduit
```

Bare `conduit` is the primary workspace. If the current Git repository is not initialized, it offers to initialize it. For initialized projects, it opens Home with searchable feature packets, lifecycle status, and selected-feature actions.

From Home you can:

- Press `n` to create a feature and jump into refinement.
- Press `/` to search features.
- Use `↑`/`↓` to select a feature, then `Enter` to choose **View**, **Refine**, **Run**, or **Status**.
- Use **View** to read packet files, **Refine** to edit and approve packet updates, **Run** to choose roles and start isolated work, and **Status** to open the latest run.

The non-verbose CLI remains available for automation, scripts, and terminals that cannot run the TUI:

```bash
conduit feature "Add team invitations"
conduit refine 001 --no-interactive "Invite teammates by email and track pending invitations."
conduit refine 001 --architect --compact
conduit run 001 --roles frontend,backend --dry-run
conduit run 001 --roles frontend,backend --compact
conduit status --tui
```

`conduit refine 001` without `--compact` opens the same interactive refinement experience used by Home. Use `--compact` when you want spinner-style architect or worker progress instead of live dashboards.

## TUI workflow

### Home

Home is the default screen opened by `conduit`. It lists Local Spec Kit feature packets from `specs/`, shows lifecycle metadata, and keeps creation, search, and feature actions mutually exclusive so keyboard focus stays predictable.

### Supported refinement flows

Conduit supports three refinement paths from Home's **Refine** action or from `conduit refine <feature-id>`:

1. **Manual packet approval**: fill in the refinement form, press `Ctrl+Enter` to preview, leave architect refinement off, then press `a` to approve and write the packet. Use this when the story is already implementation-ready and you only need Conduit to persist the packet files.
2. **Architect refinement**: fill in the form, preview, press `t` to enable the architect, optionally press `e` and `l` to cycle effort and detail, then press `a`. Conduit saves the brief, runs Codex, and opens packet review. Press `a` to approve the generated packet or `r` to request changes and send feedback back through another architect pass.
3. **Research-assisted architect refinement**: in preview, press `s` to enable repository research and `t` to enable the architect, then press `a`. The researcher gathers repository context first; review the report with `a` to accept and start the architect, `r` to rerun research, or `e` to return to the brief.

The refinement form collects problem/user story, audience, desired outcome and acceptance criteria, optional constraints, QA cases, and optional implementation guidance. Existing packet content or saved drafts are loaded back into the form when available.

When a material product or technical decision is unclear, the architect stops instead of assuming. Conduit shows its questions in a multiline answer field, stores your response in `clarifications.md`, and resumes refinement. `questions.md` is only a temporary handoff file; `clarifications.md` should be committed with the feature packet.

### Supported run flows

Conduit supports three run paths from Home's **Run** and **Status** actions or from `conduit run <feature-id>`:

1. **Plan-only run**: use `conduit run <feature-id> --roles <roles> --dry-run` to validate role selection, skill resolution, worktree setup, and launch commands without starting agents.
2. **Interactive worker run**: choose **Run** in Home, select one or more configured roles with `Space`, and press `Ctrl+Enter`. Conduit starts isolated role work and opens the worker monitor with role progress, normalized runner events, changed files, split diffs, cancellation, and review results where available.
3. **Direct run/status entry**: use `conduit run <feature-id> --roles <roles>` to start the live monitor from the shell, `--compact` for spinner-style progress, or `conduit status --tui` / Home's **Status** action to reopen recent run state.

Inside the worker monitor, use `←`/`→` or `Tab` to switch roles, `Enter` or `Space` to move from roles into activity, `j`/`k` or `↑`/`↓` to scroll activity, `t` to toggle transcript payloads, and `Ctrl+C` to request cancellation. From the files focus, `Enter` or `Space` toggles the selected file's split diff. `q` exits the monitor without cancelling a normal run.

Status shows the selected feature lifecycle and latest run. Press `Enter` from Status to open that run in the monitor.

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

## Command-line reference

The TUI is the recommended interactive path; these commands are stable fallbacks for scripts and non-TUI environments.

| Command                                              | Purpose                                                                 |
| ---------------------------------------------------- | ----------------------------------------------------------------------- |
| `conduit`                                            | Open Home, offering project initialization when needed.                 |
| `conduit init [path]`                                | Bootstrap Conduit in an existing Git repository.                        |
| `conduit feature <title>`                            | Create a feature packet from a title.                                   |
| `conduit refine <feature-id> [story]`                | Open refinement, or use `--no-interactive` with a story for automation. |
| `conduit refine <feature-id> --architect --compact`  | Run architect refinement with compact progress.                         |
| `conduit roles`                                      | List configured specialist roles.                                       |
| `conduit role resolve <name>`                        | Validate the selected role's skill source.                              |
| `conduit run <feature-id> --roles <roles>`           | Start a live worker monitor for selected roles.                         |
| `conduit run <feature-id> --roles <roles> --dry-run` | Plan commands without launching agents.                                 |
| `conduit status --tui`                               | Open the run-status dashboard directly.                                 |

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

Install that local binary onto your `PATH` with:

```bash
pnpm install:local
```

The release workflow builds Linux, macOS, and Windows binaries when a version tag is pushed.

## License

MIT. See [LICENSE](LICENSE).

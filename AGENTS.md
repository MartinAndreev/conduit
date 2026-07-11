# Conduit agent instructions

## Delivery protocol

This repository uses committed feature packets under `specs/` as the implementation contract. Read the active feature's `spec.md`, `plan.md`, applicable `tasks/*.md`, `test-cases.md`, and `contracts/` before changing code.

Only implement the approved task group named in your assignment. Do not begin a later group, change an unapproved contract, add a provider integration, or broaden scope because it seems convenient. Record a blocker instead.

## Architecture rules

- Use strict TypeScript and TSX. Keep types, enums, and interfaces out of implementation files except for narrow local inference.
- Views in `src/tui/components`, `src/tui/sections`, and `src/tui/screens` render state and delegate actions. They do not read files, spawn processes, access secrets, or implement business rules.
- State-changing work goes through a command and command handler. Read models go through a query and query handler. Do not bypass the bus from a screen.
- Core services belong in `src/system`; pure non-business helpers belong in `src/helpers/<category>`.
- Use the shared Conduit theme tokens. Do not introduce hard-coded colors in a component.
- Provider credentials never enter project configuration, run logs, prompts, snapshots, or committed files.

## Verification and handoff

Run the checks named by the active task group. Report changed files, commands run, failures, and contract risks concisely. Do not commit; the architect reviews and commits accepted groups.

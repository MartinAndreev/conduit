# Conduit agent instructions

## Delivery protocol

This repository uses committed feature packets under `specs/` as the implementation contract. Read the active feature's `spec.md`, `plan.md`, applicable `tasks/*.md`, `test-cases.md`, and `contracts/` before changing code.

Only implement the approved task group named in your assignment. Do not begin a later group, change an unapproved contract, add a provider integration, or broaden scope because it seems convenient. Record a blocker instead.

## Architecture rules

- Use strict TypeScript and TSX. Keep types, enums, and interfaces out of implementation files except for narrow local inference.
- Organize code by domain. Each domain owns `commands`, `queries`, `handlers`, `repositories`, `types`, `interfaces`, `enums`, and `errors` as needed; never add a global catch-all `src/types`, `src/interfaces`, or `src/enums` module.
- Views in `src/tui/components`, `src/tui/sections`, and `src/tui/screens` render state and delegate actions. They do not read files, spawn processes, access secrets, or implement business rules.
- State-changing work goes through a command and command handler. Read models go through a query and query handler. Do not bypass the bus from a screen.
- Core services belong in `src/system`; pure non-business helpers belong in `src/helpers/<category>`.
- Use the shared Conduit theme tokens. Do not introduce hard-coded colors in a component.
- Provider credentials never enter project configuration, run logs, prompts, snapshots, or committed files.
- Use NodeNext ESM conventions: relative TypeScript imports use the emitted `.js` specifier. Do not use `.ts` runtime specifiers, `@ts-expect-error` to mask integration problems, or `any` to bypass a type boundary.

## Verification and handoff

Run the checks named by the active task group. For runtime changes, also verify `pnpm start --help`, package build, and the appropriate standalone target. Report changed files, commands run, failures, and contract risks concisely. Do not commit; the architect reviews and commits accepted groups.

## Review correction protocol

If a group fails review once, the architect's next correction request must include concrete guidance: the violated contract or rule, affected file(s), the intended design, and the exact verification command or test to satisfy. Do not send a second vague retry. If it fails review again, stop further implementation retries and let the architect take over or explicitly narrow the remaining work.

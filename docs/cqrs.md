# Conduit CQRS

## Bus contracts

`CommandBus.dispatch(command)` routes a discriminated command to one handler and returns its result. `QueryBus.execute(query)` routes a discriminated query to one handler and returns a read model. Bootstrap registers all handlers once and rejects duplicate registrations.

Handlers receive services through typed dependencies. A handler owns validation, side effects, and errors for its use case. Formatting and parsing remain helpers; UI state remains in TUI controller hooks.

## Initial commands

- Initialize project and global profile
- Save/discard/approve a refinement draft
- Start/cancel an architect or worker run
- Set feature lifecycle status
- Save provider settings and credential-profile selection

## Initial queries

- Project bootstrap state
- Home feature list and selected feature details
- Provider availability and credential-profile state
- Draft recovery state
- Run timeline, transcript, patch, and review read models

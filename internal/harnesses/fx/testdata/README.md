# fx ACP fixtures

Sanitized responses rewritten from the shapes of fx 0.0.13/0.0.14 ACP
recordings in T3 Code (MIT). IDs are synthetic, option lists are reduced and
no frame is copied verbatim. `responses.json` maps an ACP method to the
result the fake fx agent returns; `session/prompt` updates are emitted
before the prompt result. The fake answers `session/set_config_option` with
the session/new options, updating the changed option's `currentValue`.

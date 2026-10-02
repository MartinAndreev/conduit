# Codex adapter fixtures

`codex-luna-multifile-sanitized.jsonl` is the sanitized event evidence from the
2026-09-20 bounded `gpt-5.6-luna` App Server smoke experiment. It contains no
account, credential, rate-limit, or raw reasoning records. Adapter tests convert
its deliberately reduced rows into correlated notification envelopes; it is
fixture evidence, not a complete native protocol capture.

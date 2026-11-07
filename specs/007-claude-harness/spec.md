# 007 — Claude Code харнес адаптер

Дата: 2026-11-07. Статус: **предложение; адаптерният пакет е разрешен за
разработка от Martin на 2026-11-07**. Свързването с конфигурацията, discovery и
интерфейсите не е част от този пакет. Отворените решения са в края.

## Цел

Conduit да работи с Claude Code чрез абонамента на потребителя, както работи с
Codex и OpenCode. Адаптерът стартира собствения `claude` binary на потребителя,
в който той сам е влязъл. Conduit не вижда, не съхранява и не препраща
credentials и не предлага собствен login.

## Политически граници

- Използва се само немодифицираният `claude` executable, открит при потребителя.
  Не се използват Agent SDK, OAuth token, API key или извличане на credentials.
- В средата на дъщерния процес се премахват `ANTHROPIC_API_KEY` и
  `ANTHROPIC_AUTH_TOKEN`. В `-p` режим API key има предимство пред login, а
  целта е абонаментът. Решение (Martin, 2026-11-07): абонаментът е
  единственият поддържан път; API key режим няма.
- Премахват се и `CLAUDECODE` и всички `CLAUDE_CODE_*`, както и
  `CLAUDE_EFFORT`, `CLAUDE_PID` и `CLAUDE_AGENT_SDK_VERSION`. Иначе Conduit,
  стартиран от Claude Code сесия, наследява чужда сесия, socket или effort.
- Липсващ login е видим типизиран отказ, не fallback. `initialize` връща
  `account.tokenSource`. Стойност `none` означава, че няма login, и
  `OpenSession` завършва с `ProviderError{Code: harness_unauthenticated}`.
  Ако такъв код още липсва в договора, се използва най-близкият съществуващ и
  това се отчита.

## Ownership

- `internal/harnesses/claude` притежава процеса `claude -p`, stream-json
  протокола, control requests, корелацията и нормализацията.
- Техническите правила са в `internal/harnesses/claude/rules`, със същия
  Facts/Decision{Kind,Reason} модел като Codex и OpenCode.
- `internal/process` притежава process group-а. Договорът
  `internal/sessions/provider` не се променя в този пакет. Липсващо поле се
  предава като warning, както при Codex.

## Стартиране

Една сесия е един дълготраен процес:

```
claude -p --input-format stream-json --output-format stream-json --verbose
  --include-partial-messages --replay-user-messages
  --session-id <uuid> --model <model> --permission-mode <mode>
  [--json-schema <schema>] [--settings <json>] [--disallowedTools ...]
```

- `--session-id` е нов UUID, генериран от адаптера. Това е native ID, а не
  Conduit ID. Resume (`--resume`) е отложен.
- `cwd` е workspace пътят.
- Първото съобщение е
  `{"type":"control_request","request_id":"…","request":{"subtype":"initialize","hooks":null}}`.
  Отговорът `control_response.response.response` дава `models`, `account`,
  `claude_code_version`, `current_permission_mode` и `session_state`. Те стават
  capability evidence и `HarnessVersion`.
- stderr се дренира непрекъснато. Пази се ограничена опашка от него, само за
  диагностика при неуспешен старт.
- Един JSON ред е ограничен от `MaxLineBytes` (по подразбиране 128 MiB, както
  при Codex). По-дълъг ред води до `session_lost`.

## Разрешения

Приетият режим за сесия е writable workspace с `PermissionDeny`. Той трябва да
е еквивалентен на Codex `workspace-write` с approval `never`.

- Предложение: `--permission-mode acceptEdits` и
  `--disallowedTools WebFetch,WebSearch`.
- Към това се добавя
  `--settings '{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true}}'`.
- В `-p` без `--permission-prompt-tool` всяко неразрешено искане се отказва
  автоматично и турът не може да увисне.
- Bash извън sandbox не се разрешава.
- Точните settings ключове и поведението на sandbox трябва да бъдат проверени
  на живо, преди режимът да се обяви за поддържан (виж „Отворени решения“).
  Дотогава `Capabilities` маркира permission поведението като `unknown`.
- `can_use_tool` (интерактивни разрешения) е отложено. `RespondPermission` и
  `AnswerQuestion` връщат типизиран unsupported.

## Тур, steer и прекъсване

- **StartTurn.** Изпраща се
  `{"type":"user","message":{"role":"user","content":…},"parent_tool_use_id":null,"session_id":…,"uuid":<нов UUID>}`.
  - UUID-ът на съобщението се записва като native turn input ID.
  - Отговор на ниво протокол няма. Доказателство за доставка са ехото
    `user` с `isReplay:true` и същия `uuid`, както и
    `command_lifecycle{command_uuid, state:"started"}`.
  - До едно от тях доставката е `dispatched`, без доказано приемане.
- **Steer.** Изпраща се същото съобщение, но с `"priority":"now"` и нов UUID,
  докато турът е активен.
  - UUID-ът се добавя към множеството входни UUID-и на активния тур.
  - Ехото или `command_lifecycle started` с този UUID доказва native приемане,
    но не и приложена корекция.
  - Без ехо доставката остава unknown със същата атрибуционна политика като
    Codex и OpenCode.
- **Край на тур.** Краят е `result`, чиито `user_message_uuid`/`user_message_uuids`
  пресичат множеството на активния тур и чийто `terminal_reason` не е
  `aborted_streaming` или `aborted_tools`.
  - Тези два reason-а са междинни резултати при steer и не завършват тура.
  - Решението е правило `rules.DecideResultTerminal`.
  - Резултат за чужди UUID-и не изчиства активния тур и дава warning.
- **Резултатът `result` е native наблюдение.** `subtype:"success"` с
  `is_error:true` е неуспешен тур (виж live записа, където липсващият login
  връща `terminal_reason:"api_error"`).
  - Отчита се `native_turn_error` warning с ограничен `result` текст и
    `terminal_reason`.
  - Подтипове `error_*` също са неуспех.
- **InterruptTurn.** Изпраща се
  `control_request{subtype:"interrupt"}`. Отговорът
  `{"still_queued":[…]}` е receipt, а не доказателство за спиране. Турът свършва
  само с наблюдаван `result`.
- **Други наблюдения.**
  - `command_lifecycle` със `state:"cancelled"` или друго финално състояние е
    наблюдение. Само по себе си не завършва тур.
  - `system/status` е idle/requesting наблюдение.
  - `system/init` се повтаря на всеки тур. Сменен `model` дава warning
    `model_changed`.

## Нормализация

| Native | Provider събитие |
| --- | --- |
| `stream_event` `content_block_delta` text/thinking | текстови/reasoning delta със стабилен item (message id + block index), версия и byte offset |
| `assistant` съобщение, text блок | snapshot на същия item |
| `assistant` с `model:"<synthetic>"` | warning `native_notice`, не е изход на модела |
| `assistant` `tool_use` + следващ `user` `tool_result` | Bash → command started/completed с ограничен изход; Edit/Write/MultiEdit/NotebookEdit → file activity с път от `file_path`/`notebook_path`; други → generic tool activity |
| `tool_result` с `is_error:true` | неуспешен tool; при Bash без exit code warning `command_failed` |
| `result.usage` | per-turn cumulative usage; липсващ брояч е unknown |
| `result.structured_output` при `--json-schema` | недоверен `ResultCandidate`, само ако е валиден JSON обект и резултатът е от активния тур |
| `user` с `origin.kind:"task-notification"` | wake наблюдение; не започва Conduit тур (warning `native_wake`) |
| `parent_tool_use_id != null` | sub-agent активност; атрибутира се към parent tool item, без отделен тур |
| неизвестен `type`/`subtype` | warning веднъж за вид |

Текстовите граници, offset-ите и truncation метаданните следват съществуващия
`boundedtext`. Атрибуцията към assignment следва договора. Докато steer е
unresolved, събитията са без атрибуция.

## Затваряне и загуба

- `Close` с активен тур има три варианта:
  - `explicit_detach` е отказан, защото процесът е собствен и не може да живее
    без stdin;
  - `interrupt_then_close` праща interrupt, затваря stdin и изчаква `StopGrace`
    (по подразбиране 2 s);
  - `require_quiescent` е отказан.
- След това адаптерът убива целия process group и reap-ва.
- EOF на stdout, грешен JSON или неочакван exit водят до `SessionLost`, без
  измислен native резултат.

## Извън пакета

Конфигурационна schema v2 и discovery на `claude`, wiring в app/TUI/GUI,
resume/fork, `can_use_tool`, MCP конфигурация, фонови задачи като отделни
турове, compaction събития, избор на effort и fast mode, live тестове в CI.

## Приемане

- Fake-process тестове (скрипт или in-process fake), без модел и мрежа:
  initialize, липсващ login, тур с ехо и резултат, steer с междинен
  `aborted_streaming`, чужд резултат, `is_error`, synthetic съобщение,
  interrupt receipt, close варианти, line bound и env sanitization.
- Fixtures са синтезирани от формите в live записа от 2026-11-07
  (`claude 2.1.316`, без login, затова няма отговор на модела). Не съдържат
  пътища, skills или account данни на потребителя.
- `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./internal/harnesses/...`,
  `go build ./...`.

## Live evidence (2026-11-07, claude 2.1.316)

- `initialize` response keys: `account{tokenSource,apiProvider}`, `models[]`
  (`value`, `resolvedModel`, `supportsEffort`), `current_permission_mode`,
  `session_state`, `claude_code_version` и `pid`.
- Всеки тур съдържа
  `command_lifecycle{command_uuid,state:queued|started|cancelled}`,
  `system/init`, `system/status{status:"requesting",user_message_uuids}`,
  ехо `user{isReplay:true,uuid}`, `assistant` и
  `result{user_message_uuid,user_message_uuids,terminal_reason,is_error,result_index,queued_turn_count,usage,modelUsage,model_usage_since_last_result}`.
- `interrupt` response: `{"still_queued":[]}`.
- Не са наблюдавани: реален отговор на модела, tool_use, steer по време на
  активен тур, `structured_output`, sandbox поведение. Те остават fixture-only
  до запис с login извън sandbox.

## Отворени решения

Прието (Martin, 2026-11-07): permission режимът е `acceptEdits` + sandbox за
Bash + забранени web tools. Остава live проверка на sandbox settings ключовете.

1. **Config schema v2.** Добавянето на `claude` към `configuration.Supported()`
   чупи schema v1 проверката `len(Harnesses) == len(Supported())`. Нужни са
   миграция или нова версия.
Прието (Martin, 2026-11-07): абонаментът е единственият път, без API key режим.

## Реализация — 2026-11-07

Пакетите `internal/harnesses/claude` и `rules` са реализирани с fake-process
тестове. Работата е делегирана на Opus агент, защото Sol не е наличен; Martin
одобри тази замяна. Разлики от предложението:

- `internal/process.Spec` получи `ReplaceEnv`, така че премахнатите променливи
  наистина липсват в средата на дъщерния процес, а не са празни.
- Договорът няма `harness_unauthenticated`. Липсващ login връща
  `ErrorAuthFailed` с `RetryNever`.
- Steer без доказателство за доставка към края на тура оставя сесията с
  неразрешена атрибуция. `StartTurn`/`Steer` получават `stale_binding`, докато
  не дойде късно доказателство. Това е същият проблем като при OpenCode
  `deliveryUnknown` и чака общо решение за reconcile.
- `explicit_detach` винаги се отказва. `require_quiescent` е разрешен при
  idle сесия.
- Всички native-зависими capabilities са `unknown` до live запис с login.
  Interrupt е `supported` по live evidence.

Проверки: `gofmt -l`, `go vet ./...`, `go build ./...`,
`go test -count=3 ./...`, `go test -race ./internal/harnesses/... ./internal/process/...`
и `git diff --check` минаха.

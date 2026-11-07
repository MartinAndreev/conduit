# 008 — ACP харнеси: fx, Grok Build, Cursor, Antigravity

Дата: 2026-11-07. Статус: **решения 1–3 са приети от Martin на 2026-11-07; разработката на
общия клиент и fx започва**. Grok, Cursor и Antigravity следват след приемане
на fx.

## Цел

Четирите харнеса имат една и съща stdio транспортна форма: Agent Client
Protocol (ACP), JSON-RPC 2.0 с по един JSON обект на ред. Затова предлагаме
един ACP клиент и тънки дескриптори за всеки харнес, вместо четири отделни
адаптера. Всеки харнес работи със собствения си login или абонамент. Conduit не
посредничи за credentials.

| Харнес | Команда | Login | Особености |
| --- | --- | --- | --- |
| fx | `fx acp` | собствен (`fx status --json` за проверка) | `_meta.fx.steering:true`; `loadSession`, `session/resume`, `session/close` |
| Grok Build | `grok agent stdio` | собствен xAI login | много `_x.ai/*` разширения: queue, background задачи, monitor, subagents, `prompt_complete` |
| Cursor | `agent acp` (по-стари версии: `cursor-agent`) | `cursor_login` auth method или предварителен login | блокиращи `cursor/ask_question` и `cursor/create_plan`; според външни източници няма `session/list`, `resume` или `set_model` |
| Antigravity | `agy-acp-server` (отделен binary) | изолиран login на ACP сървъра | само binary, инсталиран от потребителя; най-малко evidence |

## Предложен ownership

- `internal/harnesses/acp`: общ ACP клиент. Притежава JSON-RPC реда,
  `initialize`, `session/new`, `session/prompt`, `session/cancel`,
  `session/close`, нормализацията на `session/update` и отказа на неподдържани
  server requests. Правилата са в `internal/harnesses/acp/rules`.
- Всеки харнес е отделен тънък пакет (`internal/harnesses/fx`,
  `internal/harnesses/grok`, `internal/harnesses/cursor`,
  `internal/harnesses/antigravity`), който композира клиента. Пакетът
  притежава executable, аргументите, version parser, разпознатите разширения и
  capability overrides. Решение: Martin, 2026-11-07.
- Общият код е оправдан, защото семантиката е обща (ACP е спецификация), а не
  само синтаксисът. Разширенията (`_x.ai/*`, `cursor/*`, `_meta.fx`) остават в
  дескриптора на собственика си.

## Протоколно картографиране (общо)

- **Отваряне.**
  1. `initialize` с `protocolVersion`, `clientCapabilities`
     (`fs.readTextFile/writeTextFile:false`, `terminal:false`) и `clientInfo`.
     Отговорът дава `agentCapabilities`, `agentInfo.version` и `authMethods`.
  2. `session/new` с `cwd` = workspace и `mcpServers:[]`.
  3. Изборът на модел минава през `session/set_model` или
     `session/set_config_option`, когато е обявен. Иначе е unsupported.
- **Тур.** `session/prompt` е заявка, чийто отговор (`stopReason`, `usage`) е
  native терминалът на тура.
  - `stopReason:"cancelled"` е отменен тур.
  - `end_turn` е завършен.
  - `max_tokens`, `refusal` и `max_turn_requests` са завършени с warning.
  - JSON-RPC грешка е неуспешен тур.
- **Прекъсване.** `session/cancel` е notification. Receipt-ът е само
  „изпратено“. Турът свършва с отговора на `session/prompt`.
- **Steer.** Не е част от базовия ACP. Поддържа се само там, където
  дескрипторът го доказва с fixture (fx `_meta.fx.steering`, Grok queue).
  Другаде `Steer` връща типизиран unsupported.
- **Разрешения.** `session/request_permission` се отговаря автоматично по
  приетия `PermissionDeny`/workspace режим:
  - избира се опцията `reject_once`;
  - ако опцията липсва, отговорът е `cancelled`;
  - отказът се отчита като warning.
- **Въпроси.** `elicitation/create` и `cursor/ask_question` се отказват
  (decline). `RespondPermission` и `AnswerQuestion` остават unsupported в първия
  пакет.
- **`session/update`:**

  | `sessionUpdate` | Provider събитие |
  | --- | --- |
  | `agent_message_chunk` | text delta |
  | `agent_thought_chunk` | reasoning delta |
  | `tool_call` / `tool_call_update` | tool, command или file activity според `kind` (`execute`, `edit`, `read`…) и `status` (`pending`, `in_progress`, `completed`, `failed`) |
  | `usage_update` | usage |
  | `user_message_chunk` | ехо, без събитие |
  | `available_commands_update`, `session_info_update`, `config_option_update` | игнорирани administrative наблюдения |
  | неизвестен | warning веднъж за вид |

- **ResultCandidate.** ACP няма native schema поле. Кандидат не се създава
  (както при OpenCode).
- **Процес.** Процесът и process group-ът са собствени на адаптера. Close
  праща `session/close`, когато е обявен, затваря stdin, изчаква `StopGrace` и
  убива групата.

## Evidence

T3 Code fixtures (MIT) са суров ACP JSON-RPC от fx 0.0.14 и Grok, plus
`acp_elicitation`. Наблюдавани методи: `initialize`, `session/new`,
`session/prompt`, `session/cancel`, `session/close`, `session/set_model`,
`session/set_config_option`, `session/request_permission` и
`elicitation/create`. Наблюдавани update видове: тези от таблицата. Нашите
fixtures се пишат наново и санитизирани по тези форми. Не се копират
дословно. За Cursor и Antigravity няма записи. Нито един от четирите binary-та
не е инсталиран на машината за разработка.

## Ред на работа (предложение)

1. Общ ACP клиент + fx дескриптор. fx е най-чистото съвпадение и има fixtures.
2. Grok дескриптор, с разширенията за queue/steer след това.
3. Cursor дескриптор, след live запис на `agent acp`.
4. Antigravity, след инсталиране и запис.

Оценка: ACP клиентът е 1–2 седмици, fx 2–3 дни, Grok 3–5 дни, Cursor и
Antigravity около седмица всеки. Всеки харнес изисква live запис, преди да се
обяви поддръжка.

## Извън пакета

Config schema, discovery и UI wiring, resume/load на сесии, MCP сървъри,
файлова система и терминал от страна на клиента (`fs/*`, `terminal/*`),
интерактивни разрешения и въпроси, разширенията за фонови задачи.

## Приети решения (Martin, 2026-11-07)

1. **Layout.** Общ клиент `internal/harnesses/acp` и отделен пакет за всеки
   харнес.
2. **Ред.** fx е първи.
3. **Permission заявките.** В първия пакет се отказват автоматично
   (`reject_once`) с warning.

## Отворени решения

1. **Cursor транспорт.** Само `agent acp`, или и stream-json път за по-богати
   tool детайли?

## Реализация — 2026-11-07 (общ клиент + fx)

`internal/harnesses/acp` (с `rules`) и `internal/harnesses/fx` са реализирани
с fake agent и санитизирани fixtures. fx обявява `_meta.fx.steering`, но нито
един запис не показва формата на steer съобщение, затова `Steer` остава
unsupported. ACP `usage_update` (`used/size`) е размер на контекста и няма
поле в договора. Пренася се само цената.

Отворени след реализацията:

Прието (Martin, 2026-11-07): fx сесиите работят в режим `auto`. Дескрипторът
задава `SessionSettings` (`mode=auto`). Общият клиент ги прилага след
`session/new` и избора на модел чрез `session/set_config_option`, по правилото
`rules.DecideSessionSetting`. Ако опцията липсва, стойността не е изброена или
fx отхвърли заявката, `OpenSession` връща `capability_unsupported` и fx се
спира. Сесия никога не продължава в `ask`. По-стари fx версии, които предлагат
само `code`/`ask`, затова не се поддържат.

1. **ProviderID/Variant.** Непразен Variant се отказва. ProviderID се прилага
   само чрез единствената model config опция. Да се игнорира ли като при Codex,
   или Variant да стане fx `effort`?
2. **`fx --version`.** Изходът не е записан. Parser-ът приема само semver.

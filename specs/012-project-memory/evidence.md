# 012 — Live evidence за етап 0

Записите са без имена на потребителски MCP сървъри, plugins или съдържание на
файлове. Всяка проба ползва временен workspace и не променя настройките на
потребителя.

## Claude 2.1.316 — 2026-11-08

Login: абонамент (`authMethod: claude.ai`), `apiKeySource: none`. Всички
стартове са `claude -p ... --output-format stream-json --verbose --model haiku
--permission-mode acceptEdits --disallowedTools WebFetch,WebSearch`, без
`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` и `CLAUDECODE` в средата.

Изолационна конфигурация (I):

```
--setting-sources project,local --strict-mcp-config --mcp-config <фиктивен сървър>
--settings '{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true,
  "filesystem":{"denyRead":["~/.conduit"]}},
  "permissions":{"deny":["Read(~/.conduit/**)"]},"autoMemoryEnabled":false}'
```

| Наблюдение | Базов старт | I без deny правилата | I |
| --- | --- | --- | --- |
| Login работи, турът завършва | да | да | да |
| MCP сървъри в `system/init` | 3 потребителски | само фиктивният | само фиктивният |
| Фиктивният сървър стартира (страничен ефект) | не е зададен | 1 път | 1 път |
| `memory_paths` в `system/init` | има | няма | няма |
| Plugins в `system/init` | 3 | 3 | 3 |
| Bash: четене на `~/.conduit/config.yaml` | разрешено | разрешено | `Operation not permitted` |
| Bash: запис в `~/.conduit` | `Operation not permitted` | `Operation not permitted` | `Operation not permitted` |
| Read tool върху `~/.conduit/config.yaml` | — | — | отказан (permission deny) |
| Write tool в `~/.conduit` | — | — | отказан (Read deny rule) |

Пробният файл не е създаден в нито един случай.

### Изводи за състоянията

- **`protected_paths`: `enforced`** за конфигурация I и тази версия. Четене и
  запис са отказани и през Bash, и през Read/Write tools. Отделен запуск с
  точните аргументи на адаптера (stream-json вход, без `--mcp-config`, hash
  `9e7cac6d…`) потвърди същото. В него Glob и Grep не съществуват като tools,
  така че търсенето минава през Bash и sandbox-а. Edit и NotebookEdit не са
  доказани: пробата с Edit беше no-op и спря преди проверката на правата.
- **Без deny правилата агентът чете `~/.conduit`.** Само sandbox-ът спира
  записа. Затова deny правилата са задължителна част от launch конфигурацията.
- **`mcp`: остава `possible`.** Потребителските сървъри липсват в init, но това
  е докладвано от харнеса, а не страничен ефект от несъстояло се стартиране.
- **`memory`: остава `possible`.** Липсата на `memory_paths` е добър сигнал, но
  не е наблюдавано отсъствие на четене.
- **`hooks`: остава `possible`.** `--setting-sources project,local` не спира
  plugins, а те могат да носят hooks и skills. Механизъм за изключването им още
  няма.
- **`instructions`: остава `possible`.** Проектните инструкции са разрешени по
  политика; потребителските не са пробвани.

## Codex 0.155.1 — 2026-11-08 (непълно)

Пробите са с `codex exec`, а адаптерът ползва `app-server`, така че не са точният
launch на адаптера.

| Конфигурация | Bash четене на `~/.conduit` | Bash запис |
| --- | --- | --- |
| `-s workspace-write` | разрешено | `Operation not permitted` |
| profile с `"~/.conduit"="none"` | неустановено | неустановено |

При втория ред моделът отказа да изпълни командата заради видимата политика, така
че отказ от sandbox-а не е наблюдаван. Потребителската конфигурация на Codex се
зарежда (видим опит за връзка с потребителски MCP сървър).

Извод: `protected_paths` остава `unknown`, а агент с Codex **може да чете
`~/.conduit`** в режима на адаптера, докато не се докаже механизъм. Всички native
източници остават `possible`.

## OpenCode 2.0.22 и ACP

Не са записани. Проба за OpenCode не е изпълнена. Правилото `external_directory`
е по path и няма наблюдавано OS-ниво ограничение; това е непроверено. fx, Grok,
Cursor и Antigravity не са инсталирани.

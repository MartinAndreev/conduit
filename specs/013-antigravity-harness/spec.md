# 013 — Antigravity харнес адаптер (`agy acp`) и startup discovery

Дата: 2026-11-21.
Статус: **реализирано на клон `feature/antigravity-harness` (решения приети от Martin на 2026-11-21)**.

## Цел и обхват

Conduit да поддържа **Antigravity** чрез собствения CLI на потребителя (`agy`) и вградения му Agent Client Protocol сървър (`agy acp`), като композира общия ACP клиент (`internal/harnesses/acp`) и използва config schema v2 (`internal/configuration`).

Влиза в този пакет:

1. Тази спецификация и актуализация на `specs/008-acp-harnesses/spec.md`.
2. Добавяне на стандартната ACP административна нотификация `current_mode_update` в `internal/harnesses/acp/rules/session_update.go`.
3. Адаптерен пакет `internal/harnesses/antigravity` + санитизирани fixtures от live записа на `agy 1.5.13`.
4. Регистрация на `configuration.Antigravity` в `Supported()` (без вдигане на `CurrentSchema`, остава v2) и discovery дескриптор в `internal/startupinfra/probe_descriptors.go`.

## Приети решения (Martin, 2026-11-21)

1. **Приоритет.** Antigravity се интегрира веднага след `fx` (преди Grok и Cursor).
2. **Идентичност.**
   - `configuration.Antigravity HarnessID = "antigravity"`
   - `ProviderName = "antigravity"`, `AdapterVersion = "antigravity-acp/1"`
   - Пакет: `internal/harnesses/antigravity`
   - Default executable: `"agy"` (overridable през `acp.Config.ExecutablePath` / `config.yaml`)
3. **Транспорт.** `agy acp` по stdio JSON-RPC 2.0 през общия `internal/harnesses/acp` (заменя `agy-acp-server` от `spec 008`). Conduit не посредничи за credentials — наследява средата и cached CLI login-а (`agy` / `agy acp --login`).
4. **Режим на изпълнение.** Задължителен `mode=auto` чрез `SessionSettings` (`fail-closed`, без fallback към `default`).

## Live evidence (`agy 1.5.13`, 2026-11-21)

Проведено е live тестване на локално инсталирания `agy 1.5.13` в изолирани временни workspace директории (handshake/config/close, пълен тур с `write_file` + `run_command` + reasoning, `session/request_permission` + `session/cancel`, и неавтентикирана сесия с изолиран `HOME`).

- **`agy --version`:** `1.5.13` (гол semver).
- **`initialize`:** `protocolVersion: 1`, `agentInfo: {"name":"Antigravity ACP Server","version":"1.5.13"}`, `loadSession: true`, `sessionCapabilities.close: {}`, `_meta.antigravity: {"tier":true,"systemPrompt":{"append":true}}`, `authMethods` (`oauth-personal`, `oauth-business`, `gemini-api-key`, `agent-platform`).
- **`session/new` (неавтентикиран):** JSON-RPC грешка `{"code":-32000,"message":"Authentication required"}` → мапва се към `provider.ErrorAuthFailed`.
- **`session/new` (автентикиран):** връща `sessionId` (UUID), `models.availableModels` (комбинирани `model-effort` slug-ове), `modes` (`default`/`accept-edits`/`auto`/`plan`) и `configOptions`:
  - `id: "model"` (`category: "model"`, базови model ID-та)
  - `id: "thought_level"` (`category: "thought_level"`, динамични стойности според модела: напр. `low|medium|high|xhigh` за `gemini-4-neon`, `low|medium|high` за `gemini-3.8-flash`)
  - `id: "mode"` (`category: "mode"`, `default|accept-edits|auto|plan`)
  - Няма отделна `provider` опция (`ProviderConfigID: ""`).
- **`session/set_config_option`:**
  - `model` връща пълния обновен `configOptions` (включително стеснените `thought_level` опции).
  - `thought_level` при валидна стойност връща обновен `configOptions`; при невалидна връща `-32602`, което `acp` мапва към `capability_unsupported`.
  - `mode=auto` връща обновен `configOptions` с `currentValue: "auto"` (потвърждава се от `rules.DecideSettingConfirmation`).
- **`session/set_model`:** приема комбиниран `modelId`, връща `{}` и изпраща `config_option_update`.
- **`session/update`:** `current_mode_update`, `config_option_update`, `available_commands_update`, `session_info_update`, `usage_update` (`used`/`size`), `agent_thought_chunk`, `agent_message_chunk`, `tool_call` / `tool_call_update` (`kind: "edit"` с `locations[].path` и `diff`, `kind: "execute"` с `title` = командата и text content).
- **`session/prompt`:** `stopReason: "end_turn"` с `usage` (`inputTokens`, `outputTokens`, `cachedReadTokens`, `cachedWriteTokens`, `thoughtTokens`, `totalTokens`).
- **`session/request_permission`:** string `id` (`"1"`), опции `allow_once` (`"allow"`) и `reject_once` (`"deny"`). Отговорът `selected: "deny"` проваля tool call-а чисто и турът завършва с `stopReason: "end_turn"`.
- **`session/cancel`:** прекратява активния тур с `stopReason: "cancelled"`.
- **`session/close` + EOF на `stdin`:** връща `{}` и процесът излиза чисто с exit code `0`.

## Ownership

- `internal/harnesses/acp/rules/session_update.go`: класифицира `current_mode_update` като `SessionUpdateAdministrative`.
- `internal/harnesses/antigravity`: тънък пакет, който композира `internal/harnesses/acp` с `Descriptor()`, `New()`, `VersionCommand()`, `ParseVersion()` и санитизирани fixtures в `testdata/responses.json`.
- `internal/configuration`: добавя `Antigravity` към `Supported()` при непроменена `CurrentSchema = 2` и замразено `schemaV1Harnesses()`.
- `internal/startupinfra/probe_descriptors.go`: добавя `agy --version` probe и `parseAntigravityVersion` (гол semver или един незадължителен `"agy "` / `"v"` префикс).

## Избор на модел (`ModelSelection`)

- **Каноничен път:** базов `ModelID` от `configOptions[id="model"]` + незадължителен `Variant` от `configOptions[id="thought_level"]`.
- **Комбиниран slug:** комбиниран `ModelID` от `models.availableModels` с празен `Variant` минава през `session/set_model`.
- **`ProviderID`:** непразен `ProviderID` връща `capability_unsupported`, защото Antigravity няма `provider` config option.

## Извън пакета

- Ръчен live smoke тест в `internal/smoke/live` (Фаза 4).
- Project Memory / `protected_paths` изолация (`spec 012`, `--sandbox` / `sandbox-exec`, `--include-default-customizations=false`) — `NativeContextEvidence` остава празно (`unknown`/`possible`) до реален тест (Фаза 5).
- Turn steering, `session/resume`, интерактивни разрешения/въпроси, app/TUI/GUI wiring за избор на харнес.

## Приемане

- `gofmt -l .`, `go vet ./...`, `go test ./...`, `go build ./...`.
- Всички тестове работят детерминистично без мрежа и без стартиране на реалния `agy` binary.

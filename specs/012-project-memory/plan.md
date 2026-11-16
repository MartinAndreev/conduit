# 012 — План за имплементация

Дата: 2026-11-08, ревизиран след Astra преглед. Статус (2026-11-16): **в изпълнение**; готовото е отбелязано с ✅ по задачи.
Dispatch intent-ът е библиотечен slice (2.1, 2026-11-16); паметта още не е вградена в реален dispatch flow (4.3). Основа: [spec.md](spec.md). Клон:
`feature/project-memory`, от `feature/go-migration`.

Планът приема препоръките по шестте отворени решения от спека. Друг отговор
променя задачи 1.2, 3, 4 и етап 3.

**Изпълнител.** Решение (Martin, 2026-11-08): имплементацията и executable
тестовете на spec 012 са при Opus субагенти, като изключение от правилото за Sol
в `AGENTS.md`.

## Блокери

1. **Turso пакетите не се линкват на тази машина.** Причина (2026-11-10):
   `xcode-select` сочи Xcode 26.6, чийто linker не чете MacOSX 27.0 SDK-а от
   Command Line Tools. Заобикаляне: `DEVELOPER_DIR=/Library/Developer/CommandLineTools`
   пред Go командите; с него целият `go test ./...` минава. Постоянна поправка:
   Xcode 27 (Martin).
2. ~~Схемата няма миграции~~: решено със 3.1 (адитивна миграция 1 → 2).
3. ~~Няма dispatch процедура и intent запис~~: решено като библиотечен slice в
   2.1 (2026-11-16): `DispatchStartTurn`, `DispatchSteer` и атомарният port.
   Остава вграждането в реален flow (CLI, lifecycle, recovery по spec 003); до
   него етап 1 остава библиотечна основа.
4. ~~FTS в `tursogo v0.7.2`~~: проверено на 2026-11-10, няма FTS; виж „Търсене“ в спека.

## Задачи

### 0. Подготовка

| # | Задача | Проверка |
| --- | --- | --- |
| 0.1 | Поправка на toolchain-а, извън репото (Martin). Заобикаляне с `DEVELOPER_DIR` (блокер 1); постоянната поправка е Xcode 27 | `go test ./internal/storage/turso/` линква и минава |
| 0.2 | ✅ Проба: FTS, `LIKE`, `lower()` върху кирилица в Turso (2026-11-10) | резултатът е записан в спека |

### 1. Етап 0 — native контекст и защитени директории

Независим от останалото.

| # | Задача | Файлове |
| --- | --- | --- |
| 1.1 | Live запис с положителна контрола и реален опит за запис в `~/.conduit`; без лични данни. ✅ Claude, Codex (непълно, `codex exec` и проба през `app-server`), OpenCode (2026-11-08/09). Codex през точния launch на адаптера (2026-11-16). Остава: ACP агентите не са инсталирани; ACP; OpenCode през собствения сървър на адаптера (2026-11-16) | `specs/012-project-memory/evidence.md` |
| 1.2 | ✅ Състояния по източник и `protected_paths` в capabilities; `DecideNativeContextPolicy` и `DecideProtectedPathsPolicy` във всеки адаптер | `internal/sessions/provider`, `internal/harnesses/*/rules` |
| 1.3 | ✅ Claude флагове, след потвърждение на login и sandbox; evidence е вързан с hash-а на launch конфигурацията | `internal/harnesses/claude/launch.go` |
| 1.4 | Codex, OpenCode, ACP, където 1.1 доказа механизъм. В изпълнение (2026-11-16). Codex: `app-server` с permission profile-а (`DecidePermissionProfile`, `read-only` → проектните директории само за четене, мрежата изрично изключена), `thread/start` без `sandbox`, версия от `initialize` (`userAgent` с продукт `conduit`), launch hash; evidence за `protected_paths` е обвързан и с платформата. ✅ Codex е `enforced` за 0.155.1, `workspace-write` и macOS (live запис 2026-11-16). OpenCode: собствен `opencode serve` за всяка сесия (решение 2026-11-16) с парола през средата, `127.0.0.1` и `--server` за всички клиенти; на macOS под `sandbox-exec` (`DecideSandbox`, Seatbelt profile и за разрешения symlink път), спиране при Close, launch hash с placeholder-и. `server.info` на собствения сървър е задължително доказателство (точен URL на сесията и PID на притежавания процес, иначе отварянето се проваля); версията на харнеса е от него, а невалидна `MAJOR.MINOR.PATCH` версия оставя сесията с неизвестна версия. Steer след Close или загубена сесия се отказва; паролата се редактира от всички излъчени текстове. ✅ OpenCode е `enforced` за 2.0.22, sandbox-натия launch и macOS (live запис 2026-11-16). ACP: агентите не са инсталирани | адаптерите |

### 2. Договори (преди схемата)

| # | Задача | Зависи от |
| --- | --- | --- |
| 2.1 | ✅ (2026-11-16) Договор за dispatch intent със собственика му (spec 003) и port за атомарен запис на manifest, payload, intent. Решения на Martin (2026-11-16), всички по препоръките: `internal/execution` + `internal/execution/dispatch`; v2 на място (старите v2 бази се отказват); payload лимит 1 MiB, без рязане; trigger `sessions_binding_locked` + partial unique index, `Save` → `sessions.ErrBindingLocked`, без гаранция за смесения WAL режим; Claude Steer → `unknown/transport_only` и блокира; target turn — последният `accepted` start; блокиране от отворен intent като временна граница; `sessions.revision` временно като assignment revision; settlement с 10 s бюджет и до 3 опита само за busy провал на DB транзакцията; отделно състояние `diverged`; финализирането (COMMIT/ROLLBACK/close) без доказана граница. Виж spec 003 „Dispatch intent“ и „Manifest“ в спека. Остава: вграждане в реален flow | решение по обхвата |
| 2.2 | ✅ Типове и правила в `internal/memory`: transition, eligibility, admission, приложимост, дедупликация, с table тестове без база | — |

Ако intent не влезе в този пакет, 2.1 се отбелязва като отложен, а задачи 3.3,
4.3 и 6 се местят след него. Repository ports остават, атомарният port не.

### 3. Схема и съхранение

| # | Задача |
| --- | --- |
| 3.1 | ✅ Адитивна миграция 1 → 2 с `DecideSchemaMigration`; тестове за отворена v1 връзка и двама migrators |
| 3.2 | ✅ Таблиците от спека без outbox; регенериране на `internal/generated` |
| 3.3 | Repository ports и специализираният атомарен port; session UoW остава непроменен; optimistic concurrency. Първо пътят за запис на записи (създаване, преходи, становища, архивиране) без атомарния port за intent, който чака 2.1 (Martin, 2026-11-12). ✅ Пътят за запис на записи (2026-11-12): `DecideRecordCreation`, `DecideRecordOpinion`, `EffectiveTrust`, `internal/memory/records`, Turso адаптер. Атомарният port за manifest, payload и intent ✅ (2026-11-16, задача 2.1): `dispatch.Store`, Turso `(*Store).Dispatch()` |

### 4. Вертикален flow

| # | Задача |
| --- | --- |
| 4.1 | ✅ Redaction на входа: отчети, записи, рендериране; политика с версия (`redaction.PolicyVersion` 1) |
| 4.2 | ✅ `ComposeContext` с explicit sources, локално търсене и проверка за остаряване, с повторна проверка при commit. Налага максималния размер на входа преди redaction и пренася находките от оригиналния материал: повторна redaction на маскиран текст не дава находки. Правилото за повторната проверка (`DecideManifestRecheck`) е готово; извикването му при commit е част от 4.3 |
| 4.3 | Вграждане в dispatch, с тест за срив между manifest и intent; изисква 3.3, 4.1, 4.2, 4.4 и етап 0. Включва и премахването на payload-ите при изтриване (`memory.DecidePayloadRemoval`, обезсилване на intent-а), преместено от 4.5. Готово в 2.1 (2026-11-16): manifest, payload и intent се записват атомарно (manifest и intent в една транзакция, затова срив между тях няма); повторната проверка при commit; границата на `context_manifest_payloads.content` (1 MiB); пълното съпоставяне `execution.IntentDispatchState` и `not_dispatched`. Остава: вграждането на `ComposeContext` и workspace reader-а в реален flow и триенето на payload-ите с обезсилването на intent-а при изтриване на запис |
| 4.4 | ✅ Рендериране в `InputPart` с еднократен маркер |
| 4.5 | ✅ Изтриване: tombstone, receipts (2026-11-11). `DecideRecordDeletion`, `DecidePayloadRemoval`, `internal/memory/deletion`, Turso адаптер; граници на `context_*` колоните. Премахването на payload-ите е преместено в 4.3 |

### 5. Етап 2 — предложения и преглед

| # | Задача |
| --- | --- |
| 5.1 | Формат на предложенията, лимит, дедупликация, visibility. ✅ (2026-11-13): блок `conduit-memory`, `proposals.Extract`, `DecideProposalKeep`, `MaxProposalsPerReport`, `RecordProposals`; виж „Етап 2“ в спека. Решения на Martin (2026-11-13), приложени: одобрение на точната версия прави attempt предложението видимо за проекта (`EffectiveVisibility`); blob hash-ът е твърдение на агента, проверено само по граматика; JSON се декодира от суровия отчет и redaction е по поле; отчет над 256 KiB (`MaxReportBytes`) е `too_large`. Остава: откъде идва крайното съобщение (spec 003) и промоция на attempt записи при приемане |
| 5.2 | Преглед, обвързан с версия и hash; архивиране след 90 дни. ✅ (2026-11-13): опашка за преглед (Turso `ReadReviewQueue`), дубликати по страница, `ReviewBatch`, `records.Service.Merge` с `DecideProposalMerge`; архивирането е `ArchiveExpiredProposals` от 3.3. Одобрението прави предложението видимо за целия проект (ефективна visibility, Martin, 2026-11-13); дедупликацията и опашката остават на съхранения scope. Остава: UI (5.4), дубликати между страници, извикване на архивирането по график |
| 5.3 | Оптимизация на проверката за остаряване с кеш по snapshot. Предпоставка ✅ (2026-11-14): истински workspace `SnapshotReader` (`internal/memoryinfra.WorkspaceSnapshots`, hash чрез `git hash-object --stdin-paths`, по един git процес на пакет), пакетен port `ObserveFiles`, правила `DecideSnapshotPath` и `DecideSnapshotFile`, `MaxSnapshotFileBytes`; виж „Snapshot на workspace-а“ в спека. Кеш ✅ (2026-11-15): stat кеш в паметта на `WorkspaceSnapshots` (вариант а; TTL 10 мин, 8192 записа LRU, racy защита), виж „Кеш (5.3)“ в спека. Остава: вграждането на reader-а (с 4.3) |
| 5.4 | TUI и GUI „Контекст и решения“; „Какво ще получи агентът“ само след 4.3 |
| 5.5 | Команда за одит на паметта с агент: агентът преглежда паметта на проекта и предлага изтриване или архивиране на ненужни записи като становища; човек потвърждава всяко изтриване (Martin, 2026-11-11). Домейнна част ✅ (2026-11-13): `memory.Suggestion` и колона `memory_reviews.suggestion` (v2 на място, с CHECK само за становище на агент); материал за агента (`audit.AuditMaterial`, Turso `ReadAuditMaterial`, `active` и `needs_verification`, до 100 записа и 256 KiB съдържание) и `render.RenderAudit`; блок `conduit-memory-audit` (`audit.Extract`, `DecideSuggestionKeep`, общ `reportblock` с предложенията); `RecordSuggestions` през `DecideRecordOpinion` с предложение; чакащи предложения (`PendingSuggestions`, `DecideSuggestionStanding`); потвърждение по едно (`ConfirmSuggestion`, `DecideSuggestionConfirmation`, после `ArchiveWithin` или `DeleteWithin` в същата транзакция); виж „Одит на паметта“ в спека. Отхвърляне на предложение от човек ✅ (2026-11-15, решение 7а): `DismissSuggestion` и `DecideSuggestionDismissal`, ред `decision = 'dismiss'` в `memory_reviews` (v2 на място), обвързан с версията, hash-а и действието; отхвърленото не е чакащо, потвърждението му дава `dismissed`, а агентът получава `suggestion_dismissed` за същото действие и версия. Остава: пускането на агента, командата и източникът на крайното съобщение (spec 003); UI за чакащите предложения, потвърждението и отхвърлянето (5.4) |

### 6. Метрика

Базовата линия от спека, след 4.3. Резултатът решава дали започва етап 3.

### 7. Етап 3 — Engram (само след метриката)

Първо проверка срещу текущия релийз: hard delete и `operation_id` през MCP, и
какво остава в SQLite файла. После адаптер, инсталатор, reconciler и настройката
в UI. Подробна разбивка при старта.

## Ред

```
0.1 → 0.2 → 2.2 → 3.1, 3.2 → 4.1, 4.2, 4.4, 4.5 → 5.1–5.3
2.1 (spec 003) ✅ → 3.3 ✅
3.3, 4.1, 4.2, 4.4, етап 0 (1) → 4.3 → 5.4, 6 → 7
```

## Проверки и процес

- Всяка задача: `gofmt`, `go vet ./...`, `go test ./...`, `go build ./...`;
  `go test -race` за новите пакети; `git diff --check`.
- Astra преглед на финалния diff на задачи 2, 3 и 4.
- Commit-и и push само при поискване.

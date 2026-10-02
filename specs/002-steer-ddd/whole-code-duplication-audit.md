# Duplication и shared-access audit на целия текущ Go код

## Приоритети за човешкия преглед

1. **Codex input conversion носи грешния operation за Steer.** Реален съществуващ дефект в общ adapter-local helper: [internal/harnesses/codex/session.go:206](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/session.go:206). Минимална отделна промяна може да премахне operation policy от конверсията.
2. **Session mutex вече има един технически собственик, но wrapper не уеднаквява effect scope.** [internal/harnesses/opencode/session.go:72](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/session.go:72) още стартира owned process вътре в critical section; [internal/harnesses/codex/session.go:165](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/session.go:165) и двата Steer освобождават mutex преди доставка. Запазено поведение и архитектурен въпрос за отделна задача, не нов race, установен от миграцията.
3. **Корелацията на обикновени Codex observations все още е отделена от event snapshot.** Candidate path вече има един snapshot, но text/tool/terminal пътищата проверяват active binding преди по-късното emit. Това е потенциал за несъгласувано attribution при concurrent transition, без изпълнена конкретна репродукция.
4. **Schema compatibility knowledge и journal decoding изискват координирани промени.** Handwritten schema + validateSchema и двете Event row декодирания са най-полезните следващи storage reuse граници. Генерираният Jet код не е clone finding.

Прегледът е advisory. Няма автоматично приемане, блокиране или поправки.

## Констатации и минимални препоръки

### D1 — P2: общата Codex input конверсия е обвързана с StartTurn

**Доказан дефект, предшестващ последните промени.** StartTurn ([internal/harnesses/codex/session.go:160](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/session.go:160)) и Steer ([internal/harnesses/codex/steer.go:17](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:17)) използват codexInputs. При InputArtifactRef или друг unsupported kind helper-ът връща ErrorCapabilityUnsupported с OperationStartTurn на [internal/harnesses/codex/session.go:213](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/session.go:213). Steer директно препраща тази грешка; caller получава wrong operation въпреки правилния rejection-before-dispatch. Това е наблюдаема structured-error некоректност, доказана от правия control flow; не добавях тест за репродукция.

**Сценарий за промяна:** нов input kind или различна capability policy за correction трябва да работи и в двата use case; поправка само в един caller би оставила грешна диагностика/typed matching в другия. **Собственик:** Codex adapter. **Минимална граница:** codexInputs приема operation, или връща adapter-local conversion error, който процедурата превежда. Не е нужен global input validator. Фокусиран тест: unsupported input през двата публични entry points, вярна Operation, RemoteEffectNotSent и нула dispatch.

### D2 — висок архитектурен приоритет: еднакъв mutex механизъм, различни effect граници

**Подобрение/решение, без нов доказан дефект.** Всички handwritten production Lock/Unlock са само в [internal/harnesses/internal/lockscope/mutex.go:10](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/internal/lockscope/mutex.go:10). Wrapper-ът е синхронен и освобождава при return/panic. Но OpenCode StartTurn извиква NewID и Launcher.Start под s.mu ([internal/harnesses/opencode/session.go:85](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/session.go:85), [internal/harnesses/opencode/session.go:91](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/session.go:91)); ObserveState също извиква injectable Now под s.mu ([internal/harnesses/opencode/session.go:60](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/session.go:60)). Codex и Steer отделят delivery от state mutex. Wrapper коментарът изисква harness I/O извън scope, докато наследеният OpenCode launch е запазен вътре. Това не се поправя чрез замяна на Lock/Unlock синтаксиса.

**Сценарий:** бавен launcher или бъдещо блокиращо callback поведение задържа ObserveState/Close/Steer за целия launch. Ако просто изнесем Start извън mutex без reservation, два callers могат да стартират два processes. **Собственик:** OpenCode start procedure; минимално reserve-under-lock → launch step → accept/rollback-under-lock, с предварително одобрени close/cancellation/partial-success правила. Не общ harness skeleton. Решение от Martin е нужно за scope и failure policy; това е нова задача. Фокусирани checks: blocked launcher + конкурентен Start/Close/Observe, точно един dispatch, reservation recovery при launch error.

### D3 — P2 потенциал: check и snapshot са различни shared-state access пътища

**Подобрение/непотвърден race, запазена извън поправения candidate област.** [internal/harnesses/codex/normalize.go:156](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/normalize.go:156), [internal/harnesses/codex/normalize.go:254](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/normalize.go:254), [internal/harnesses/codex/normalize.go:104](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/normalize.go:104) използват matchesActive/matchesThread или отделно прочитат active, след което s.emit създава envelope от нов snapshot чрез emitEvent ([internal/harnesses/codex/session.go:417](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/session.go:417)). Candidate вече повторно валидира native turn, thread и contract вътре в body builder и използва същия snapshot за body/envelope ([internal/harnesses/codex/normalize.go:198](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/normalize.go:198)).

**Сценарий:** стар native delta/terminal е проверен при turn A; concurrent Close изчиства active или друг transition променя attribution преди publication; обикновен event body може да остане observation за A, а envelope да е от новите факти. Не съм възпроизвеждал този interleaving, затова не го обявявам за доказан дефект. **Минимална граница:** Codex-local correlated emit builder, който валидира native target срещу същия eventBinding, който оформя envelope. Native parsing/text offset tracking остават местни; изисква се решение коя наблюдавана ревизия трябва да носи receipt/terminal. Фокусиран deterministic тест като текущия candidate test, но със state transition между normalize check и публикацията на text/terminal. Не разширявайте мълчаливо atomic scope с I/O под s.mu.

### D4 — среден приоритет: ръчно schema knowledge в две места

**Подобрение, не генериран clone и не доказано нарушение на текущия bootstrap договор.** Schema.Statements ([internal/storage/turso/schema/schema.go:8](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/storage/turso/schema/schema.go:8)) определя tables/columns/UNIQUE(run_id,sequence); validateSchema има отделна ръчна required-column list ([internal/storage/turso/store.go:69](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/storage/turso/store.go:69)). Production createSchema и dev generator правилно изпълняват една и съща Statements колекция. Generated tables са output, не независима schema authority.

**Сценарий:** нова journal колона или промяна в uniqueness изисква schema, regeneration, validation и handwritten codecs/CAS tests. Пропуснато validation обновяване може да пропусне несъвместима база до първата заявка; текущият validation проверява column presence, не constraint equivalence. **Собственик:** storage schema/migration boundary. Минимално при сегашния bootstrap: явно adapter-owned compatibility descriptor и тест, който съпоставя required shape с fresh schema. Не парсвайте SQL текст чрез generic helper и не използвайте Jet generated code като startup authority. Storage spec вече отлага versioned migration runner/checksums; изборът на runner/constraint compatibility е за Martin, не разрешена промяна от този audit.

### D5 — нисък/среден приоритет: journal row decoding е поддържано два пъти

**Подобрение.** Append(existing-ID branch) ([internal/storage/turso/events.go:38](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/storage/turso/events.go:38)) и Replay ([internal/storage/turso/events.go:86](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/storage/turso/events.go:86)) независимо изброяват същите EventJournal scan fields и RFC3339Nano decoding. Append също има sameEvent policy ([internal/storage/turso/events.go:70](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/storage/turso/events.go:70)), която правилно игнорира allocated Sequence и сравнява caller content.

**Сценарий:** нов persisted provenance/observed field се добавя в единия decoder; replay и dedup read model могат да се разминат или Scan да се провали. **Минимална граница:** private turso scanEvent(rowScanner) за row decoding; Append и Replay запазват query/error context, transaction и dedup flow. sameEvent остава domain-specific content comparison, не generic struct equality. Checks: append→dedup→replay roundtrip на всички persisted fields, malformed observed time и same-ID/different-content conflict. Не споделяйте универсален decoder със sessions, които имат други time/version invariants.

### D6 — въпроси за общ domain език, не предложение за автоматично обединяване

[internal/sessions/assignment.go:5](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/sessions/assignment.go:5) има sessions.RunID, [internal/events/repository.go:13](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/events/repository.go:13) има events.RunID, а sessions.Record държи RunID/ProjectID/AttemptID/AssignmentID като string ([internal/sessions/repository.go:21](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/sessions/repository.go:21)). Provider alias-и правилно не дублират AssignmentRef. При бъдеща промяна на run identity mapping orchestration ще трябва да конвертира тези representations. Днес няма потребителска orchestration, която доказва drift. Martin трябва да реши дали durable journal RunID и assignment RunID са една domain identity или намерено отделни понятия; не въвеждайте global IDs package само за намаляване на повторението.

## Shared-access карта и области, които трябва да останат отделни

| Ресурс/инвариант | Текущи access пътища и guards | Извод |
| --- | --- | --- |
| Correction identity/revision | DecideCorrection; двата reserveSteer под session mutex | Един domain rule; transport variations остават local |
| Codex pending delivery | start/steer failure handlers, late callbacks, classifyDeliveryFailure | Обща native-rejection policy вече е single-owner; state release местна |
| Codex event attribution | emitEvent → snapshotEventBinding; candidate builder | Candidate/body envelope имат един snapshot; D3 за други correlations |
| OpenCode lease/process | StartTurn, Close, readRun, Steer, emit/failID | s.mu guards state; D2 effect scope; shared process ownership не е shared server ownership |
| RPC pending map | call/remove/abandon/deliverResponse/finish чрез wrapper | Atomic detach под c.mu; callback и channel delivery извън него |
| Broker buffer/terminal | Publish/Finish/Subscribe/Receive/Close чрез wrapper | Nonblocking publish и explicit overflow са в един owner; Receive остава single-reader contract |
| Turso connection/transaction | Store factories, UoW tx-bound ports, event append transaction | SQL е само storage; app/domain нямат raw DB bypass |
| OS process pipes/reap | ExecLauncher/trackedReadCloser/Wait; адаптерите drain | Един process abstraction; Unix tree kill и Windows root-only са умишлено отделни |

Няма основание да сливаме Codex RPC call/notify/rejectRequest в generic workflow: call има pending correlation и abandonment, notify няма response. Сходният JSON envelope код не оправдава скриване на uncertainty sequencing. Codex close убива owned App Server/process group, OpenCode close не убива shared service; тези потоци не са общ cleanup skeleton. Text normalization/capabilities/default config са adapter-specific facts; общите ID generation, typed provider event construction, boundedtext и adaptererror вече имат тесни собственици. Празните event marker methods/type switch и compile-time fake interface implementations са contract boilerplate, не повторена business policy. Не препоръчвам abstraction заради броя функции или сходни редове.

## Проверка на трите предходни теми върху текущия код

- **Session handle:** Codex reserveSteer вече извиква checkSessionReference в критичната секция; чужд handle се отхвърля преди reservation/dispatch. Прочетох TestSteerForeignSessionDoesNotReserveOrDispatch и го изпълних чрез focused suite.
- **Candidate/envelope snapshot:** snapshotEventBinding се взима под eventMu → s.mu; candidate builder и envelope ползват един snapshot. Deterministic TestCandidateAndEnvelopeShareSnapshotAcrossSteerTransition проверява reserve-after-snapshot и accept-after-unresolved-snapshot; лично изпълнен успешно. D3 не отменя този конкретен fix.
- **Native rejection classification:** StartTurn и Steer използват classifyDeliveryFailure; late start/steer различават rejection от transport uncertainty. TestNativeRejectionPolicyMatchesStartAndSteer изпраща реален fake RPC rejection през двата entry points и проверява code/effect/retry/cause/request ID. Current behavior е проверено, не заключено от старите audit доклади.

## Scope, snapshot и доказателства

Repo: `/Users/martinandreev/Projects/open-source/conduit-go/conduit-go`. След изричния final code freeze прочетох текущите mutable harnesses отново. Snapshot обхваща **70 Go файла: 63 handwritten, 7 generated** под internal/ и cmd/, включително nested dev generator. SHA-256 на JSON manifest със sorted relative paths→file hashes: `ca2ebf306916b75a454183f42d59865c7a3afdfebc31a84935b0638e80682e69`. Manifest: [whole-code-snapshot.json](whole-code-snapshot.json). Повторната проверка след анализа не намери променени Go файлове. Manifest е точният snapshot идентификатор, независимо от parent documentation/staging изменения.

Прочетени са AGENTS.md, architecture/verification, duplication-audit skill, Steer spec, harness contract, storage contract. Имената/пътищата са обходени чрез rg; няма добавян clone detector/tooling. Прегледани са **всички handwritten production Go implementation/contract файлове**: cmd main, app persistence, events/session repositories и identity/rule/provider contract, id/process (включително Unix/Windows branches), storage adapter/schema/generator, всички Codex/OpenCode implementations и shared harness helpers. Generated code е разгледан само за generator/table/model boundary и schema ownership, не line-by-line clone audit.

Тестовете в целия scope са инвентаризирани и всички root module tests изпълнени; ръчно са прочетени релевантните state/correlation/concurrency/failure, storage/UoW/CAS/idempotency, rule/contract и wrapper assertions. Не претендирам, че всяка fixture/test helper assertion е била отделно одитирана за стил. Fixture JSON payload-ите не са самостоятелен duplication scope. Няма непрегледан handwritten production package; няма отделна helpers папка в текущото Go дърво. Изключени са deleted TypeScript, node_modules, generated clones, външни зависимости и production data. Няма live/network/model calls, code/test/docs edits или commit от този reviewer.

### Лично изпълнени проверки в този whole-code audit

Всички използват `GOCACHE=/tmp/conduit-go-cache TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache`:

| Команда | Резултат |
| --- | --- |
| CGO_ENABLED=0 go test -count=1 ./internal/sessions/... ./internal/storage/turso/... ./internal/id ./internal/process | PASS преди final harness freeze, стабилен scope |
| nested generator: CGO_ENABLED=0 go test ./... | PASS compile, no test files; generator не е изпълняван |
| go test -race -count=1 ./internal/harnesses/... ./internal/sessions/... | PASS след final freeze, без result cache |
| CGO_ENABLED=0 go test -count=1 ./... | PASS след final freeze, включително disposable storage tests |
| go vet ./... | PASS след final freeze |
| rg Lock/Unlock върху handwritten non-test internal/cmd | единствените два call site-а са wrapper implementation |
| snapshot повторно сравнение | 0 променени Go файла |

Parent съобщи integrated build/help успех, но не го броя за лично изпълнен в този audit. Earlier Steer audit проверки са исторически и не са текущо доказателство. Няма изпълнен Windows runtime test, exhaustive interleaving proof, нова D1/D3 репродукция или schema mutation test от reviewer. Passing suite не доказва липса на race/duplication; предложенията остават отделно решение и работа.

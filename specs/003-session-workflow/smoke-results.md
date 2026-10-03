# Smoke резултати

Тези проверки упражняват test-only композиция и реалните adapter граници. Няма production application service, GUI/TUI, recovery или автоматично приемане на реален резултат.

## Fake harness и реална временна Turso база

Пет сценария преминаха: verification с отделно симулирано human acceptance; stale candidate; unattributed candidate; неверен действителен файл; commentary/idle без candidate. Проверени са корекция с revision +1, запис на event body, отделни verification/acceptance записи и повторно отваряне на базата.

Координаторът повтори `TestWorkflowSmoke -count=1 -v` успешно. Sol изпълни успешно full suite, vet, build и CLI help с `CGO_ENABLED=0` и временни Go/Turso caches. Race изпълнението на sessions с Turso не стартира: host linker не разбира `arm64e.x1` в macOS SDK. Това не е успешен race резултат.

## Защита на live теста

Default test discovery показва само безопасните gate тестове. При едновременно `CI=true`, `GITHUB_ACTIONS=true`, live build tag, opt-in и разрешен model ID `TestManualLiveHarness` се пропуска преди ефекти. Независимият агент повтори безопасните tagged gate проверки и прегледа cleanup/candidate correlation.

Live командите са в [manual runner README](../../internal/smoke/live/README.md). На този host проверките използват `CGO_ENABLED=0`, `GOCACHE=/tmp/conduit-go-cache` и `TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache`. Няма GitHub Actions workflow за live изпълнение. CI presence блокира изпълнението дори когато стойността е празна или `false`.

## Реални изпълнения

| Harness | Модел | Резултат | Наблюдение |
| --- | --- | --- | --- |
| Codex | gpt-5.6-luna | PASS, 12.01 s | Точното съдържание на единствения файл е проверено независимо; валиден native candidate с attribution; 22 sanitized journal събития |
| OpenCode | opencode/mimo-v2.6-flash-free | FAIL при OpenSession, 0.13 s | StartTurn не е извикан; задачата не е изпратена към модела; няма автоматично повторение |

Codex cleanup: `handle_closed=true`, `resources_released=true`, `execution=none`.

И при двете live проверки `application_accepted=false`: кандидатът не е application acceptance. Live steering не е тестван, защото кратката задача няма надежден checkpoint; steering е проверен във fake сценария.

Точните model IDs са потвърдени чрез metadata без inference. Старият MiMo v2.5 free ID липсва; използван е наличният v2.6 flash free в рамките на разрешения избор MiMo free. Няма платен fallback. Сурови provider логове, account данни и reasoning не са записвани в доклада. Временните workspace и журнал се изчистват от теста.

Причината за OpenCode OpenSession отказа е установена с две metadata-only session-create проверки, без prompt/inference: тестът е поискал празен model variant, а услугата е върнала `default`. Адаптерът правилно връща `protocol_violation` с `RemoteEffectAccepted`, защото сесия е създадена, но конфигурацията се различава. Поправката е изричен `Variant: default` само в smoke конфигурацията, без промяна на production проверката. На 2026-10-03 потребителят разреши още един live опит; резултатът е описан по-долу.

Поправеният runner задава изрично OpenCode variant `default`. Добавената диагностика показва само stage и типизирани code/effect/retry/cause-class, без оригиналния error text. Безопасните tagged тестове и vet минаха след поправката. CI refusal остава непроменен.

Двете metadata-only диагностични сесии нямат turns. Идентификаторите им не са запазени, затова не е правен опит за широко или недоказано безопасно изтриване. Споделената услуга не е спирана.


## Разрешен повторен OpenCode опит — 2026-10-03

Изпълнен е точно един допълнителен опит с `opencode/mimo-v2.6-flash-free`, `Variant: default`. OpenSession и StartTurn този път не върнаха грешка, но наблюдението приключи по timeout след 150.01 s без terminal evidence.

- Резултат: FAIL; `file_verified=false`, `native_candidate_observed=false`, `application_accepted=false`.
- Записани са 2 sanitized journal събития; 0 text, 0 tool-completed, 0 file-activity. Това не доказва причината за липсващата работа.
- Workspace/artifact проверките не са изпълнени успешно; очакваният файл не е потвърден.
- Cleanup: `handle_closed=true`, `resources_released=false`, `execution=unknown`. Няма отчетена грешка от cleanup на проследените собствени процеси; това не доказва спиране на remote/shared изпълнението.
- Live steering не е проверен. Няма автоматично повторение, платен fallback или нова inference заявка след timeout.

Следваща диагностика трябва да установи защо CLI observer не предоставя активност; текущите данни не позволяват да се обвини моделът, transport-ът или adapter normalization поотделно. Нови live опити не са разрешени от този резултат.

## Допълнителен разрешен опит и диагностика — 2026-10-03

Потребителят разреши още един live опит, следван от диагностика. Изпълнението със същия MiMo free/default отново завърши с FAIL след 150.01 s: 2 journal събития, 0 text/tool/file activity, без потвърден файл. Cleanup върна затворен handle, `resources_released=false`, `execution=unknown`. Няма автоматична следваща inference заявка.

Диагностиката проверява отделно transport/CLI lifecycle. От кода е установено, че ExecLauncher създава StdinPipe, а OpenCode StartTurn подава prompt като аргумент и оставя stdin отворен. Дали CLI чака EOF се проверява без model prompt; това не е доказана причина само от четенето на adapter-а.

### Потвърдена причина

Sol установи механизма чрез read-only преглед на инсталирания OpenCode v2.0.22. В embedded JavaScript на `~/.opencode/bin/opencode`, offset 139542625, `run` изчаква `process.stdin.isTTY ? void 0 : await JR()` преди избиране на сесия/dispatch. На offset 119449966 `JR()` използва `text(process.stdin)` от `stream/consumers` и чака EOF. Offset-ите са за тази конкретна версия на binary, не устойчив API.

Нашият `internal/process/exec.go:22` създава stdin pipe, а `internal/harnesses/opencode/session.go:96` стартира `run` без да затвори stdin. Prompt-ът е аргумент, но CLI чака допълнителния stdin преди да го изпрати. Двете journal събития са локални SessionOpen/TurnActive, не доказателство за model dispatch.

Последните две точно филтрирани synthetic smoke сесии имат free model/default, нулев cost и нулеви token counters. Message/status API диагностиката не върна използваеми данни, затова няма допълнително твърдение за native status. Не е направена нова inference заявка, session creation или cleanup на споделена услуга. Точните synthetic IDs са запазени само в временен диагностичен файл, не в този документ.

Предложена отделна поправка: OpenCode StartTurn затваря stdin непосредствено след успешен launch и обработва close failure изрично. Не променяме глобално ExecLauncher, защото Codex използва двупосочен stdin. Regression тестът трябва да стартира локална fixture програма, която не излъчва JSONL до EOF — така проверката не зависи от модели и е подходяща за CI. Production кодът още не е променен.

## Реализирана stdin поправка — 2026-10-03

След изрично разрешение OpenCode StartTurn вече затваря stdin на стартирания CLI преди да отчете успешен старт. Codex и общият ExecLauncher са непроменени. При close error отделна failure процедура запазва delivery uncertainty/cause, блокира нов старт и прави best-effort cleanup на owned процеса извън mutex. Изчакването е ограничено до 2 секунди; unreaped process ownership не се изтрива преждевременно.

Новият `internal/harnesses/opencode/stdin_test.go` използва реален локален subprocess, който излъчва synthetic JSONL едва след EOF. Sol изпълни теста срещу предишния код: FAIL след 3.01 s без JSONL; след поправката focused OpenCode race suite мина (4.675 s). Покрити са close failure след реално EOF, no-retry, cleanup извън mutex и kill failure с blocked Wait, retained ownership и последващо reap.

Координаторът изпълни успешно full suite с `CGO_ENABLED=0`, vet, build и CLI help. Build показва нефатално sandbox предупреждение за Go module stat cache; командата завършва с exit 0. Тази промяна не е проверена с нов live model опит и не е commit-ната. Предишните live timeout резултати остават исторически валидни за кода преди поправката.

## OpenCode live след stdin поправката — 2026-10-03

Потребителят разреши един нов опит. Реалният Go adapter с `opencode/mimo-v2.6-flash-free`, variant `default`, премина smoke теста за 15.54 s.

- `file_verified=true`: независимо потвърден единствен файл answer.txt с точно очакваното съдържание.
- 7 sanitized journal събития: 1 text, 1 tool-completed и 1 file-activity.
- `native_candidate_observed=false`, `application_accepted=false`: очакваната OpenCode граница е запазена; успешната проверка на файла не се представя за candidate или application acceptance.
- `steering=not_tested`: live задачата няма checkpoint за корекция.
- Cleanup: `handle_closed=true`, `resources_released=false`, `execution=unknown`; това не е доказателство за quiescence на shared service. Няма отчетен failure от cleanup на runner-owned процесите.

Един разрешен опит, без автоматично повторение или платен fallback. Предишните timeout-и остават доказателства за дефекта преди stdin поправката. CI/build-tag/opt-in/model guards не са променяни. Няма commit.

## Two explicitly authorized live steering attempts — 2026-10-03

User requested two live steering tests. Added a manual tagged `TestManualLiveSteering`, using the existing exact-model opt-in and CI gates. Each attempt uses exactly one StartTurn and one Steer, no retry or fallback. A runner-owned bounded local checkpoint establishes BLUE and a live foreground command before correction; revision advances from 1 to 2. The test independently reads GREEN afterward. This remains test-only adapter validation, not production orchestration or application acceptance.

| Harness / model | Result | Evidence | Cleanup |
| --- | --- | --- | --- |
| Codex / gpt-5.6-luna | PASS, 27.92 s | BLUE + active checkpoint; transport accepted revision 2 targeting native_turn_id; GREEN verified; native candidate verified against revision 2, turn, schema and payload | handle closed, resources released, execution none |
| OpenCode / opencode/mimo-v2.6-flash-free, default variant | PASS, 21.57 s | BLUE + active checkpoint; transport accepted revision 2 targeting session_scope_with_adapter_lease; GREEN verified; no candidate channel | handle closed, resources not proven released, execution unknown |

OpenCode idle is observer evidence, not proof of remote quiescence. No application acceptance occurred for either provider. Shared OpenCode service was not stopped. No raw native output, account data, credentials or reasoning were retained. Temporary workspaces are removed by Go testing.

Before live execution: safe checkpoint/redaction checks passed, forced CI invocation skipped before effects, and a fresh Sol implementation audit found no substantive defect. Advisory improvement: assert OpenCode native receipt ID explicitly in the test in addition to the adapter's existing validation. The manual checkpoint requires POSIX shell/PID checks. Default CI tests do not include liveharness. No commit requested or made.

Invocations used the README commands for `TestManualLiveSteering`, with `CGO_ENABLED=0`, `GOCACHE=/tmp/conduit-go-cache`, and `TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache` for this host.

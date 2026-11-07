# 010 — Находки от независимите Go ревюта

Дата: 2026-11-07. Два независими агента **gpt-6.1-sol, reasoning high**
прегледаха предложението и относимия Go код с чист контекст. Първият
проверяваше протокол, persistence и lifecycle; вторият — продуктов обхват,
Council, authority и съвместимост с execution flow.

Общ извод: ограничен консултативен Council е разумна посока. Не е доказана
готовност за универсално cross-harness изпълнение, approval/dispatch gates
или произволни вече отворени външни сесии. Прегледите са статични; няма
изпълнени тестове или live заявки. Този запис проследява корекциите в спека,
не твърди, че проблемите вече са решени в код или че финалният пакет е
получил повторно независимо приемане.

Baseline: Go checkout при HEAD `c3019587ffc00c2832a39e7698aa2f78d2f12834` с
активни промени на друг агент. Наблюденията за адаптери са моментна снимка
на работното дърво и подлежат на повторна проверка при планиране.

| ID | Находка / сценарий на отказ | Отговор в spec | Приемателни сценарии |
| --- | --- | --- | --- |
| R01 | Claude/OpenCode/ACP отказват read-only; prompt или последващ clean diff не предотвратяват write/ambient effects | Safe mode е prerequisite; неподдържан режим не се заменя с writable. Свежи сесии и ограничен snapshot | C02–C07, Q02–Q03 |
| R02 | Native ResultCandidate като изискване за всяка реплика изключва полезно общуване; обратното — JSON в commentary дава фалшив ACK | Отделни съобщения, валидирани приноси и формални agreement responses; квалифицирани канали, без text-to-approval | C10–C12, N05 |
| R03 | Candidate идва преди StartTurn response или преди failed terminal; късно абониране губи events | Един event consumer преди dispatch; provisional capture, точен binding, проверка на terminal/gaps преди приемане | C08–C12 |
| R04 | Приключил turn оставя жив процес; лимитът един процес се нарушава или другият участник чака безкрайно | Отделни turn/process budgets; Close и потвърдено освобождаване, bounded unknown cleanup с наблюдаем owner | C17–C18, C22, N11 |
| R05 | Два dispatcher-а четат един durable intent; уникален command ID не предотвратява два външни ефекта | Атомарен claim с expected version и journal; само победителят dispatch-ва; crash outcome е unknown без автоматичен replay | C19–C21, N09 |
| R06 | Синтезът изпуска или обявява за разрешено неудобно възражение; мнозинството създава привидна сигурност | Всички валидирани възражения са отделно видими и open по подразбиране; evidence или човешко приемане на риск | C13–C16, Q04 |
| R07 | Private input за A изтича към B в свободния текст при reveal, въпреки филтрирани refs | Общ разрешен набор за всички получатели и синтезатора; различни изследователски въпроси върху същия набор | C05, C07, C27 |
| R08 | Collaboration freeze на v1 се разминава с вече одобрен v2 план или mutable файл е сменен преди launch | Един execution-plan owner; точни одобрени версии и immutable launch input; agreement е evidence, не втори authority | N02, N06–N10 |
| R09 | Смяна на native процес губи валиден ACK, или късен retry приема от името на заменен участник | Stable participant/membership отделно от turn/attempt; revision-bound ACK и stale attempt rejection | C10–C11, N03–N04 |
| R10 | Ново съобщение се използва като Steer и променя assignment; unknown delivery се release-ва/повтаря автоматично | Реплика към следващ ход; контролирана correction revision +1 остава отделна; неизвестността се пази | C20, C23 |
| R11 | Feature се представя като готов parallel team scheduler или attach към произволен външен чат | Етап 1 е managed advisory Council; етап 2 е preparation agreement; writers/външни клиенти имат отделни договори | C25, N07–N13, Q03 |

## Локални източници на проверените предпоставки

- [Provider contract и доверие към резултати](../../internal/sessions/provider/README.md)
- [Session, ResultContractRef, ResultCandidate и cleanup](../../internal/sessions/provider/contract.go)
- [Claude eligibility](../../internal/harnesses/claude/provider.go)
- [OpenCode eligibility](../../internal/harnesses/opencode/provider.go)
- [ACP eligibility](../../internal/harnesses/acp/provider.go) и [MCP setup](../../internal/harnesses/acp/session.go)
- [Codex configuration](../../internal/harnesses/codex/config.go), [turn lifecycle](../../internal/harnesses/codex/session.go) и [normalization](../../internal/harnesses/codex/normalize.go)
- [Event stream broker](../../internal/harnesses/internal/eventstream/broker.go)
- [Текущ session/event unit of work](../../internal/app/persistence.go)
- [Одобрение на конкретна spec версия](../003-session-workflow/contracts/architecture-refinement.md)
- [Последователен execution flow и бъдеща паралелна работа](../003-session-workflow/contracts/execution-orchestration.md)
- [Контекст и memory граници](../004-project-navigation/contracts/project-memory.md)

## Какво не се пренася от първото проучване

Първото проучване погрешно разглеждаше TypeScript repository-то. Неговите
конкретни findings за `state.db.lock`, замразен `AgentResponseV1`, role slots
и scheduler групи не са Go findings и не са blockers в този feature spec.
Общите рискове са включени само когато Go договорите или независимите Go
прегледи ги обосновават.

Преди implementation задачите се уточняват техническите решения от
[spec.md](spec.md), а конкретните fixes/нови capabilities получават собствен
обхват и проверки. Тази документация не разрешава поправки по активната
работа на другия агент.

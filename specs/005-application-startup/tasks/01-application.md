# A — обща startup логика

Статус: одобрен за имплементация от Sol. Прочети spec.md, plan.md, contracts/startup.md и test-cases.md, както и AGENTS.md и приложимите инструкции.

Owner: Sol implementation агент. Обхват: новите configuration/startup/application/infra пакети според договора; focused tests. Без TUI/GUI зависимости, реални inference calls, Engram installer, project catalog или refactor на harness session flows. Съществуващото help/version поведение се запазва.

Преди adapter implementation провери конкретните локални version commands, current process abstraction и cross-platform atomic publish механизма. Не заменяй непотвърдена гаранция с shortcut. Ако договорът изисква архитектурна промяна, върни concrete blocker.

Delivery: traceable procedure, specific Kind/Reason rules, едно логическо действие на step, bounded I/O, тестове S01–S18. Отчет: changed files, изпълнени проверки, OS coverage, limitations. Без commit. След това независим audit и walkthrough по repo skills.

Допълнително одобрен narrow scope (2026-10-03): Windows Job Object ownership в `internal/process`, suspended launch → assign → resume, kill-on-close и failure cleanup; Windows-only descendant/handle tests и Windows compile checks. Без други process refactors. Native Windows runtime проверката се отчита отделно от cross-compilation.

Одобрена review корекция (2026-10-03): споделен `internal/lockscope` с миграция на старите imports; разделяне на файлов query/decode/result mapping и detector lookup/execution/cleanup/classification; отстраняване на повторени harness eligibility проверки; bounded observable cleanup owner според допълнението в `contracts/startup.md`. Архитектът обновява coding стандартите след преглед на корекцията. Няма UI или commit.

# A — обща startup логика

Статус: одобрен за имплементация от Sol. Прочети spec.md, plan.md, contracts/startup.md и test-cases.md, както и AGENTS.md и приложимите инструкции.

Owner: Sol implementation агент. Обхват: новите configuration/startup/application/infra пакети според договора; focused tests. Без TUI/GUI зависимости, реални inference calls, Engram installer, project catalog или refactor на harness session flows. Съществуващото help/version поведение се запазва.

Преди adapter implementation провери конкретните локални version commands, current process abstraction и cross-platform atomic publish механизма. Не заменяй непотвърдена гаранция с shortcut. Ако договорът изисква архитектурна промяна, върни concrete blocker.

Delivery: traceable procedure, specific Kind/Reason rules, едно логическо действие на step, bounded I/O, тестове S01–S18. Отчет: changed files, изпълнени проверки, OS coverage, limitations. Без commit. След това независим audit и walkthrough по repo skills.

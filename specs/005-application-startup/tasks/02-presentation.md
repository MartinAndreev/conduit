# B — два presentation адаптера

Статус: блокиран до готов и тестван A и приемане от потребителя. Не започва чрез одобрение само на този документ.

TUI owner: един Sol агент. GUI owner: отделен Sol агент. Shared startup contracts са read-only за тях; нужните промени се връщат към application owner.

И двата адаптера показват startup и welcome, подават cancel/retry и превеждат structured diagnostics. Не извършват filesystem/process операции от views. Не обещават функционални „Добави проект“/„Отвори проект“ до отделния project flow: непокритите действия са ясно unavailable, а не fake success.

TUI: първо keyboard/focus/resize и lifecycle. GUI: templUI 2.0 beta.10; compatibility spike и избор на shell/bridge преди production wiring. Не добавяй web server/Wails или друга desktop зависимост като неявно решение.

Проверки: presentation сценарии от test-cases.md, runtime captures, visual audit, независим review. Desktop build не бива да блокира standalone CLI/TUI. Не пипайте едновременно go.mod; dependencies се интегрират от архитекта след преглед. Без commit.

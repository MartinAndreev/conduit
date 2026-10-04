# B — два presentation адаптера

Статус: одобрен от потребителя след приемане на A и commit `50afe16` (2026-10-03).

Одобрена реализация: `internal/tui` с Bubble Tea v2; plain `conduit` отваря TUI, а help/version остават без startup ефекти. GUI е отделен `cmd/conduit-gui` с Wails v2, templUI beta.10 и локални assets. Първо се доказва съвместимостта на точната templUI версия и native bridge; няма CDN. GUI adapter-ът живее в `internal/presentation/gui`, desktop dependencies не блокират standalone CLI/TUI.

При неприключил cleanup Retry е недостъпен. Exit отменя активната операция и изчаква ограничено; при непотвърден cleanup показва „Изчакай“ и изрично „Излез сега“. Observer-ът остава притежаван докато приложението работи. Няма автоматичен retry или обещание за cleanup след принудителен изход.

Езици — одобрено: английски по подразбиране и български, с общ i18n каталог за двата интерфейса. `internal/presentation/i18n` притежава устойчивите ключове и вградените en/bg ресурси; Go и GUI ползват един източник. Launch опция `--lang en|bg`, без добавяне на настройка към schema v1. Неподдържан locale използва английски. Макетите на български не определят default езика на продукта.

TUI owner: един Sol агент. GUI owner: отделен Sol агент. Shared startup contracts са read-only за тях; нужните промени се връщат към application owner.

И двата адаптера показват startup и welcome, подават cancel/retry и превеждат structured diagnostics. Не извършват filesystem/process операции от views. Не обещават функционални „Добави проект“/„Отвори проект“ до отделния project flow: непокритите действия са ясно unavailable, а не fake success.

TUI: първо keyboard/focus/resize и lifecycle. GUI: templUI 2.0 beta.10; compatibility spike и избор на shell/bridge преди production wiring. Не добавяй web server/Wails или друга desktop зависимост като неявно решение.

Проверки: presentation сценарии от test-cases.md, runtime captures, visual audit, независим review. Desktop build не бива да блокира standalone CLI/TUI. Не пипайте едновременно go.mod; dependencies се интегрират от архитекта след преглед. Без commit.

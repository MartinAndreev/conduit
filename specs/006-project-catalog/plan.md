# План

Статус: потребителят разреши работа и потвърди обща глобална Turso база на 2026-10-04. Реализацията на група A е прегледана и потребителят разреши commit на текущото състояние. Група B още не е започната.

## A. Решения и application логика

1. Изпълнение на приетите решения в `spec.md`.
2. Фиксиране на data contract, storage механизъм, package ownership и failure semantics.

Приетият Turso вариант е в [storage договора](contracts/storage-proposal.md). Той отчита текущата session schema и запазва нейния отделен жизнен цикъл.

3. Application операции и чисти правила с fake ports.
4. Реални filesystem/storage adapters с временни директории и конкурентни тестове.
5. Независим review и проверка на запазването на съществуващия startup/config.

## B. Отделни presentation задачи

След приемане на A: TUI и GUI получават отделни задачи за добавяне, списък, начално възстановяване и диагностики. Общите contracts не се редактират конкурентно. Всеки executable остава самостоятелен.

## C. Проверка и предаване

Fake fixtures за празен каталог, успешна регистрация, дубликат, липсващ последен проект, отказ от запис и повреден каталог. Реални UI captures, keyboard/cancel/resize проверки, en/bg, default standalone build и отделен native desktop build. Проверените платформи се отчитат по действително изпълнените тестове.

Project switching, агенти, spec editor и премахване/преместване на регистрации не се започват като продължение по подразбиране.

## Проверки на група A — 2026-10-04

Реализирани са domain/application операциите, filesystem адаптерът и отделният Turso каталог. Интерфейсите и composition roots още не ги извикват. Потребителят разреши commit след представянето на резултатите и driver ограничението. Driver race остава описан в storage договора; разрешението за commit не променя техническата гаранция и не отстранява проблема.

Проверени команди (временни fixtures, без реални потребителски проекти):

- `CGO_ENABLED=0 go test -count=1 ./...` — премина след корекциите.
- `go test -race -count=1 ./internal/projects ./internal/app/projects ./internal/projectinfra ./internal/storage/turso/...` — премина на macOS с CGO и SDK26.5.
- `CGO_ENABLED=0 go vet ./...` и `CGO_ENABLED=0 go build ./...` — преминаха. Build изведе nonfatal предупреждение за недостъпен запис на module stat cache; race linking изведе SDK version предупреждения.
- Cross-build на новите пакети с `GOOS=windows/linux GOARCH=amd64 CGO_ENABLED=0` — премина. Native Windows/Linux изпълнение не е проверено.
- `git diff --check` — премина.

За Go проверките е използван `GOCACHE=/private/tmp/conduit-startup-review-go-cache`; за Turso — `TURSO_GO_CACHE_DIR=/private/tmp/conduit-startup-review-turso-serial`. Native race използва `SDKROOT=/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.5.sdk`. Default SDK27 linking е ограничение на локалния toolchain.

Коригирани при интеграцията/review: задържане на недостъпните записи в списъка, typed dangling preference, повторна проверка на финалната директория, schema token boundaries и вярно `Persisted` при cancellation около COMMIT. Независимият audit приключи чрез отделен Sol агент: остава P2 driver race при външно изтриване преди open. Малка advisory препоръка: concurrent duplicate outcome да обновява и Reason според окончателния Existing Kind. Корекциите за directory replacement, commit cancellation и DDL token boundaries са повторно проверени. Commit на текущата реализация е изрично разрешен от потребителя на 2026-10-04.

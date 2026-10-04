# План — първо логиката, после приложенията

Статус: етап A е приет и записан в `50afe16`. Етап B е одобрен с TUI и Wails v2/templUI beta.10 GUI, първо GUI compatibility проверка.

## A. Application

1. Фиксиране на schema, ports, snapshot/outcome и fixtures според одобрения договор.
2. Sol имплементира domain правила и startup процедура с fake ports.
3. Sol имплементира файлов/detection adapters и интеграционни сценарии в временни директории. Споделените contracts имат един owner; промените им не се правят конкурентно от агенти.
4. Интегрирани проверки, независим чист Sol audit за DDD, човешка четимост, correctness и дублиране; architect review и walkthrough.
5. Представяне на резултата. Няма UI имплементация преди готова и тествана обща логика и приемането ѝ.

## B. Два паралелни агента след A

- TUI агент: Bubble Tea/Bubbles/Lip Gloss startup и welcome, progress/failure/retry/cancel, директно използване на application contract.
- GUI агент: първо минимална desktop/WebView compatibility проверка с фиксиран templUI 2.0 beta.10; после същите startup/welcome състояния по изчистения GUI макет. Desktop shell/bridge трябва да са приети преди production интеграция. При блокер TUI може да напредва независимо.

Всеки притежава само своите presentation файлове и тестове. Parent интегрира composition roots и dependency промени след преглед; няма паралелно редактиране на go.mod или shared contracts. App logic corrections се връщат на owner-а, не се дублират в UI.

## C. Проверка на интерфейсите

Споделени fixtures за състоянията. TUI visual audit и GUI capture спрямо макетите; функционални keyboard/focus/cancel/retry сценарии. Генерираната снимка не е runtime validation. CLI build не зависи от native desktop libraries. Native platform matrix се отчита по реално проверените платформи.

## След този slice

Проектен каталог/home, редактор, role settings и Engram download са отделни задачи. Паралелна визуална разработка на тези flows не се разрешава само защото има макети.

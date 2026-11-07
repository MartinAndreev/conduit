# 011 — Вграден браузър с CEF: табове, профили и агентски достъп

Дата: 2026-11-07. Проект: **Conduit Go**.

Статус: **потвърдена продуктова посока; feature spec за обсъждане, без
разрешение за имплементация**. Martin поиска браузър вътре в прозореца на
Conduit, проучване на CEF с Wails и macOS прототип. Последващото уточнение
включва табовете, профилите и останалите обсъдени възможности в общия обхват.
Етапите определят реда за доказване и доставка, а не отпадане на функции.

Този документ не одобрява конкретна CEF версия, Wails fork, Go package
layout, persistence schema или implementation група. Подготовката му е
документационна; не са инсталирани зависимости и не е изпълнен native spike.

## Цел и основни сценарии

Conduit предоставя браузър в собствения си desktop прозорец, в който човекът
отваря линкове, работи с няколко сайта и профила и може да даде на агент
достъп до същото видимо състояние. Потребителят може да маркира текст,
елемент или област от страницата и да изпрати конкретна инструкция към
активния агент, например „коригирай отстоянието на този бутон“.

Основни сценарии:

1. Отварям линк от разговор или адресната лента в избран таб и профил.
2. Влизам в приложение с отделен работен профил; сесията се запазва между
   стартиранията на Conduit според настройките на профила и сайта.
3. Давам на конкретен агент достъп до избран таб; виждам действията му и
   мога да поема управлението или да отнема достъпа.
4. Маркирам елемент, добавям бележка и изпращам прегледан контекст към
   избрания агент. При локален проект агентът може да промени кода през
   обичайните си инструменти и да провери резултата в същия браузър.
5. Отварям втори профил за друг акаунт, без смесване на cookies и site data.
6. Използвам login popup, качвам файл или свалям резултат, като Conduit
   показва получателя, профила и състоянието на операцията.

Браузърът не е отделен външен прозорец по подразбиране. Отваряне в системния
браузър остава изрично действие и допустим път за конкретни authentication
flows, които не позволяват embedded user agent.

## Обхват и критерий за завършен feature

| ID | Възможност | Изискван резултат |
| --- | --- | --- |
| BR-01 | Native вграждане | Страницата е интерактивен CEF изглед в Wails прозореца; resize, focus, keyboard и accessibility са проверени |
| BR-02 | Навигация | URL, back/forward/reload/stop, title/loading/error състояния, localhost и външни HTTPS сайтове |
| BR-03 | Табове | Създаване, активиране, пренареждане, затваряне, reopen и възстановяване на запазени табове |
| BR-04 | Профили | Именувани постоянни и временни профили; отделни cookies/site data, избор на профил и управление на данните |
| BR-05 | Login и OAuth | Обикновен login, redirects, popup flows, MFA и ясно поведение при неподдържан embedded OAuth |
| BR-06 | Агентски достъп | Изрично предоставен достъп до конкретни табове, наблюдение, действия, отнемане и capability reporting |
| BR-07 | Селекция и корекция | Текст, DOM елемент или област със screenshot и бележка към конкретен агент |
| BR-08 | Реални страници | Same-origin и cross-origin frames, динамичен DOM и ясно описани ограничения за недостъпни targets |
| BR-09 | Файлове | Upload picker, download destination, progress, cancellation, errors и безопасен избор на файлове |
| BR-10 | Възстановяване | Обработка на browser/renderer failure, възстановяване на табове и профили без автоматично повторение на действия |
| BR-11 | Платформи | macOS първо; Windows и Linux остават в feature обхвата със собствени native проверки |
| BR-12 | Разпространение | Пакетиране, подписване/notarization където е приложимо, съвместими CEF обновявания и измерени ресурси |

Feature-ът не се отчита като завършен само защото BR-01 работи или
детерминиран адаптер успешно управлява тестова страница. Всеки ред трябва
да има evidence и матрица на поддръжката; ограничение се описва явно и не
премахва изискване без ново продуктово решение.

## Връзка с текущия проект

Проверената основа използва Go 1.26 и Wails v2.15.0. Входът за GUI е
[`cmd/conduit-gui/main.go`](../../cmd/conduit-gui/main.go), native macOS
настройките са в [`macos.go`](../../cmd/conduit-gui/macos.go), а desktop
границата е описана в [GUI README](../../internal/presentation/gui/README.md).
Native dependencies са отделени от CLI/TUI чрез build tags; това се запазва.

Съществуващият [provider договор](../../internal/sessions/provider/contract.go)
е отправна точка за session identity, steering и input artifacts. Наличието
на метод или поле не доказва поддръжка от всеки harness. Транспортът за
browser tools и доставката на annotation трябва да бъдат доказани по адаптер.

Проучването е върху branch `codex/go-startup` с паралелни неприети промени.
Преди реализация се фиксира нов baseline; този документ не приема чуждите
промени и не променя текущите harness договори.

## Продуктово поведение

### Браузърен панел и табове

- Панелът съдържа табове, видим профил, адресна лента и навигационни контроли.
  Разговорът и страницата могат да се виждат едновременно. Точният layout
  и shortcuts се уточняват с UI проект преди реализацията на този етап.
- Линк от разговор може да се отвори в текущ или нов таб. `target=_blank`
  и popup заявки се обработват с видима политика; не се губят тихо.
- Всеки таб има стабилна Conduit identity, профил, текущ URL и lifecycle.
  Смяната на активния таб не пренасочва вече изпратена агентска команда.
- Нов таб от дадена страница наследява нейния профил. Наследяването на
  агентски права е отделно решение; предложение: новият таб няма такива
  права, докато не бъде включен в разрешения обхват.
- Затваряне на един таб не затваря Wails прозореца или другите табове.
  `beforeunload`, pending действия и downloads имат видим резултат.
- Възстановяването след рестарт връща запазените URLs, ред и профили, но не
  обещава възстановяване на предишния JS heap или непубликувани form values.
  Не изпълнява отново tool calls, uploads или form submissions.

### Профили и данни

- Постоянният профил съхранява browser state между стартиранията; временният
  не се записва като профил за възстановяване. Имената и активният профил
  са видими както за човека, така и в разрешения агентски контекст.
- Профилът е контейнер за идентичност в сайтовете, а не Conduit агентска
  сесия. Няколко таба могат да използват един профил и неговия login state.
- Различни профили не споделят cookies, local storage и cache. Предложената
  CEF реализация използва отделни request contexts и storage директории;
  реалната изолация се доказва с два акаунта и restart fixture.
- Смяна на профила на таб означава нов browser context и навигация, а не
  пренасяне на неговите cookies. При непубликувана форма има предупреждение.
- Потребителят може да създава/преименува профил, да изчиства site data и да
  изтрива профил след показване на засегнатите табове. Активен context не
  се изтрива под работещ браузър. Същата storage директория не се отваря
  конкурентно от два Conduit процеса.
- Cookies, tokens и profile директории не се копират в project config,
  git, prompts или обичайни логове. Няма автоматичен импорт от личен Chrome.
- Временният профил не означава липса на screenshots, изтеглени файлове или
  вече изпратени annotations. UI трябва да прави тази разлика ясна.

Отворено продуктово решение е дали именуваните профили са глобални за
Conduit или принадлежат на проект. Предложение: профилите са локални за
Conduit, а проектът пази избор/асоциация; това още не е прието storage решение.

### Login, OAuth и разрешения на сайтовете

Login и OAuth остават в обхвата, включително redirects, popup/opener
поведение и MFA. Паролите и MFA се въвеждат от човека по подразбиране;
authentication не дава автоматично агентски права върху други табове.

Не се обещава, че всеки доставчик допуска embedded OAuth. Например Google
ограничава OAuth requests в developer-controlled embedded user agents.
За такива случаи Conduit показва причината и поддържания външен flow, когато
такъв съществува. Login в системния браузър **не** означава автоматично
пренасяне на сесията в CEF; няма cookie extraction или заобикаляне чрез
подмяна на user agent. Съвместимостта се описва по доставчик/сайт.
[Google OAuth policy](https://developers.google.com/identity/protocols/oauth2/policies)

Permissions за camera/microphone/location/clipboard и други browser
ресурси трябва да имат видим origin и профил и да се управляват от човека.
TLS грешки не се заобикалят автоматично; локалната разработка има отделно,
явно конфигурирано поведение. Конкретните defaults се уточняват преди етапа.

### Агентски достъп и управление

- Човекът избира агент и табове и вижда дали разрешава само наблюдение или
  взаимодействие. Правата не следват неявно активния UI таб или целия профил.
- Минималните инструменти покриват list/open/activate/close tab, navigate,
  snapshot/read, screenshot, click, type, scroll и изчакване на условие.
  Управлението на нови табове изисква съответния разрешен обхват.
- Всяка операция носи session, tab и request identity; действията по
  snapshot/елемент носят и document generation и frame identity.
- Предложение за управление: един активен агентски controller на таб;
  човешко takeover прекратява бъдещи агентски действия. Read-only наблюдатели
  може да са повече от един. Тази политика подлежи на потвърждение.
- При отнемане на достъп не се започват нови ефекти. Вече dispatch-ната
  операция може да е изпълнена; cancellation/timeout не доказва rollback.
  Не се повтарят автоматично click, submit, upload или друго действие с ефект.
- Всяко действие има видим краен резултат или явно неизвестен резултат.
  Navigation, затворен таб, renderer crash и capability mismatch се различават.
- Полученото от страница съдържание е недоверен контекст, не инструкция за
  разширяване на правата. Страниците нямат общ достъп до Wails/Go bindings.
- Конкретните harness адаптери обявяват доказани capabilities. Липсваща
  поддръжка се показва, без тих fallback към друг агент или платен provider.

### Маркиране и изпращане към агент

Потребителят избира текст, DOM елемент или правоъгълна област, добавя бележка
и вижда preview на прикачвания контекст. Бутонът показва получаващия агент;
смяна на активния разговор по време на подготовката не сменя тихо адресата.

Предложеният annotation запис съдържа:

- annotation identity, избран session/turn recipient и бележка;
- tab/profile identity, URL, време, document generation и frame identity;
- избран текст, ограничен DOM excerpt, locator кандидати и bounding box;
- screenshot/crop, viewport/scroll/scale данни за интерпретиране на координатите;
- налична source reference и нейния произход, само когато може да бъде доказана.

HTML, текст и изображения имат размерни лимити и видимо truncation
поведение. Скритите form values и password fields не се добавят като
DOM контекст; preview позволява премахване на чувствителни части. Screenshot
може да съдържа чувствителни видими данни и не се счита за автоматично редактиран.

След navigation или промяна на DOM snapshot-ът остава исторически контекст,
но не е валиден target за нов click без повторно разрешаване/проверка.
Cross-origin/OOPIF target-ите се адресират по frame/target identity, не чрез
предположение за достъп от top-level JavaScript. При недостъпен DOM остава
явно обозначен screenshot контекст, а не измислен selector.

Изпращането различава подготвено, изпратено, прието, отказано и неизвестно
състояние. При agent без mid-turn steering контекстът се предлага за следващ
turn с видим статус, вместо UI да твърди, че текущият turn е коригиран.
Повторен transport опит не трябва да създава две приети annotations.

Автоматичното съпоставяне DOM → source не е гарантирано за произволен сайт.
За локални проекти се проучва като част от workflow-а за корекции; източникът
и увереността се показват, а липсата му не блокира screenshot/DOM обратна връзка.

### Upload, download и възстановяване

- Upload използва избрани от човека файлове или изрично разрешен файлов
  обхват за агента. Origin, tab, profile и избран файл се виждат преди изпращане.
- Download показва източник, избрана цел, progress и крайно състояние.
  Конфликт с наличен файл не води до тихо презаписване. Partial files,
  cancellation и повторен опит имат изрично поведение; файл не се стартира автоматично.
- Browser grants не дават общ достъп до filesystem. Файловите операции
  минават през ограничените application/native adapters.
- Crash на renderer се показва за засегнатите табове; recover/reload не
  повтаря агентски команди. При рестарт се валидира profile metadata и се
  отчита partial restore. Агентският контрол не се възстановява неявно.

## Предложена техническа архитектура

```text
Един Wails NSWindow
├── WKWebView: Conduit UI, разговор, табове, профили, контроли
└── Native container: CEF browser NSView за видимия таб
       ↕ Objective-C++ bridge / C ABI / cgo
    Go application procedures и browser port
       ↕ конкретен harness tool adapter
    Агентът работи със същия browser target като човека
```

Browser host е в процеса на Conduit; CEF renderer/GPU helpers са отделни
процеси. CEF child `NSView` използва Alloy style. Background табовете имат
самостоятелен lifecycle; hide/suspend/discard политиката предстои да се избере,
без да се обещава запазен live DOM след discard.
[CEF macOS window API](https://github.com/chromiumembedded/cef/blob/master/include/internal/cef_types_mac.h)

Предложен ownership, без фиксирани Go package имена:

| Собственик | Отговорност |
| --- | --- |
| Browser domain | Табове, профили, grants, annotations и допустими преходи |
| Application procedures | Open/close, grant/revoke, capture/send и file flows; sequencing и публични outcomes |
| Native CEF adapter | Contexts, browser objects, main-thread dispatch, CDP, view lifetime и helpers |
| Persistence adapter | Conduit metadata, restore и artifact references; не копира CEF cookies в catalog |
| GUI presentation | Табове, адресна лента, profile selection, annotation preview и контролен статус |
| Harness adapter | Реалното предоставяне на browser tools и контекст към конкретния provider |

Профилните данни се управляват от CEF, а Conduit пази свои идентичности и
metadata. Persisted profiles/tabs изискват storage договор, но таблици и
миграции още не са избрани. Преди реализация се описват Profile, Tab,
AccessGrant, Annotation и OperationResult с versioning и retention, като
всеки тип защитава конкретна идентичност, достъп или delivery invariant.
Не се въвежда общ workflow engine или нова глобална инфраструктура.

Предложението е in-process CDP чрез `ExecuteDevToolsMethod` и
`AddDevToolsMessageObserver`, без отворен debugging port. Това не обещава
автоматична Playwright съвместимост. CEF contexts са кандидатът за profile
изолацията; mapping-ът се сверява с избрания SDK.
[CEF DevTools API](https://github.com/chromiumembedded/cef/blob/master/include/cef_browser.h),
[CEF request contexts](https://github.com/chromiumembedded/cef/blob/master/include/cef_request_context.h)

### macOS startup, event loop и shutdown

1. Native bootstrap създава CEF-съвместим `NSApplication` преди Wails да
   извика `sharedApplication`. Използва се `CefAppProtocol` и изискваното
   `sendEvent` поведение. Wails `OnStartup` не е bootstrap hook: работи в Go goroutine.
2. Малък patch върху фиксирания Wails v2.15.0 предоставя main-thread/window-ready,
   layout и close hooks. Първият layout е съседни views, не HTML iframe.
3. Wails/AppKit притежава единствения main loop. CEF работи с external pump,
   scheduler callbacks и периодично `CefDoMessageLoopWork` на главната нишка.
   Не се допуска recursive pumping; modal/tracking loops се проверяват отделно.
4. C++ притежава CEF references; C ABI пренася копирани, ограничени данни и
   opaque identities. Native UI callback не чака Go/harness I/O.
5. Close първо спира приемането на нови операции и започва CEF unload flow.
   Custom Alloy `DoClose` потиска стандартното затваряне на общия `NSWindow`
   и планира teardown на притежавания child view извън callback стека.
   Wails host и pump остават живи. Точният retained-view/deallocation
   механизъм трябва да бъде доказан с SDK, без private `WindowDestroyed` API.
6. `OnBeforeClose` забранява нови browser операции и освобождава browser refs,
   но не е гаранция за последен callback. Callback owner остава жив до
   приключил shutdown и изчерпване на queued native deliveries.
7. При app quit, след затваряне на всички browsers, pump задачите се спират/
   обезсилват и `CefShutdown` се извиква на main thread извън CEF callback/pump
   стека. Следват окончателният Wails teardown и освобождаването на owners.
   При close само на таб CEF runtime продължава да обслужва останалите.

Отказ от `beforeunload` оставя таба/приложението отворено. Timeout не се
представя като успешен cleanup. Lifecycle и command-completion състоянията
не се губят при препълнена event queue; progress може да се обединява.
Повторен Quit не стартира втори cleanup owner.

Тези изисквания включват установените при независимия преглед рискове от
циклично чакане при close, late callbacks и загубен terminal outcome.
Разширеният feature spec не е минал нов независим implementation review.
[CEF application protocol](https://github.com/chromiumembedded/cef/blob/master/include/cef_application_mac.h),
[CEF pump API](https://github.com/chromiumembedded/cef/blob/master/include/cef_app.h),
[CEF lifecycle](https://github.com/chromiumembedded/cef/blob/master/include/cef_life_span_handler.h),
[Wails startup](https://github.com/wailsapp/wails/blob/v2.15.0/v2/internal/frontend/desktop/darwin/frontend.go),
[Wails shutdown ordering](https://github.com/wailsapp/wails/blob/v2.15.0/v2/internal/app/app_production.go)

### Packaging, updates и платформи

- Фиксират се CEF binary version, source/API revision, checksum, архитектура,
  минимална OS версия, SDK и toolchain. Четените `master` източници не са pin.
- Главният executable и C++ helpers зареждат framework динамично. Helper
  sandbox се инициализира преди framework loading. Sandbox не се изключва
  тихо, за да мине демонстрацията.
- Helper bundles, resources, framework symlinks и role-specific entitlements
  следват избраната CEF distribution. Local ad-hoc launch и Developer ID /
  notarization / Gatekeeper validation са отделни доказателства.
- Chromium security updates имат собствен regression и release процес.
  Обновява се съвместимият app/framework/helper комплект; profile format
  съвместимостта се проверява и downgrade не се обещава без доказателство.
- Windows и Linux имат platform adapters и native build/runtime acceptance,
  включително focus, dialogs, profile isolation и packaging. macOS pass или
  cross-build не доказват тяхната поддръжка.
- Измерват се app size, startup, idle CPU, memory на таб/профил и остатъчни
  процеси. Бюджетите се договарят след baseline измервания; налични са два
  двигателя — системният Wails webview и CEF Chromium.

[CEF macOS example](https://github.com/chromiumembedded/cef/blob/master/tests/cefsimple/cefsimple_mac.mm),
[CEF helper entrypoint](https://github.com/chromiumembedded/cef/blob/master/tests/cefsimple/process_helper_mac.cc),
[CEF code signing](https://github.com/chromiumembedded/cef/blob/master/docs/code_signing.md)

## Етапи и условия за продължаване

| Етап | Резултат | Условие за приемане |
| --- | --- | --- |
| A — macOS native spike | Изолиран експеримент, packaged app, един browser view, bootstrap/pump/shutdown | Реално native evidence за lifecycle; решение дали Wails patch е поддържим |
| B — Табове и профили | Множество табове, постоянни/временни профили, restore и popup ownership | Изолация на акаунти, restart, close на един таб без ефект върху останалите |
| C — Контекст и управление | Селекция, annotations, browser service и детерминиран тестов controller | Човекът и controller-ът работят със същото състояние; stale/revoke/overflow проверени |
| D — Реални harnesses | Browser tools и annotation delivery към избрани Conduit агенти | End-to-end evidence по harness, включително unsupported и mid-turn/next-turn поведение |
| E — Реални web workflows | Външни сайтове, login/OAuth, frames, uploads/downloads и recovery | Матрица на сценарии с проверени ограничения, без скрит fallback |
| F — Доставка и платформи | macOS release packaging/updates и native Windows/Linux adapters | Подписани пакети където е приложимо, native acceptance и ресурсни измервания |

Това е предложен ред, не разрешение за изпълнение. Етап A е ограничен
технически spike, а не продуктова версия без табове и профили. Предложеното
място е `experiments/cef-macos/` със собствен module и локален Wails replace;
не е създадено. След spike-а се избира production layout и миграционен план.
Ако се налага голям Wails fork, следва архитектурно решение, а не автоматично
разширяване на patch-а. Wails v3 е възможност за оценка, не предварително условие.

## Приемателни проверки

Това са планирани проверки; нито една не е изпълнена с този документ.

| Проверка | Наблюдаем резултат |
| --- | --- |
| Native startup и 20 open/close цикъла | Няма crash/deadlock или останали собствени helpers; ресурсите са измерени |
| Quit, повторен Quit, pending command, unload отказ | Ясен резултат; няма premature host teardown или callback към освободен owner |
| Бавен Go consumer и пълен mailbox | Terminal lifecycle/completion не се губят; UI нишката остава responsive |
| Resize, Retina, zoom, scroll, кирилица/IME | Верни координати, focus и screenshot/selection съвпадение |
| Context menus, native select, autofill и modal loops | Няма starvation; избор с мишка и клавиатура работи |
| Три таба, close/reopen/restore и popup | Стабилни identities, правилен профил, останалите табове продължават |
| Два профила със същия origin | Различни cookies/storage, правилен login след restart, без cross-profile leakage |
| Временен профил и cleanup failure | Няма restore на профила; остатъчните artifacts/cleanup failure са отчетени |
| Concurrent app/profile open и изтриване на активен профил | Няма двойно владение или изтриване под активен context |
| Annotation, сменен активен агент и navigation | Фиксиран получател, верни attachments, stale target е отказан |
| Cross-origin frame, OOPIF, динамичен DOM | Frame/target mapping е верен; unsupported е явен, без грешен top-level click |
| Grant/revoke, човешко takeover и два агента | Няма скрито пренасочване на таб или неразрешен втори controller |
| Timeout след click/submit и renderer crash | Няма автоматично replay; неизвестният ефект остава неизвестен |
| Harness без tools или mid-turn steering | Capability отказ/next-turn доставка се виждат; няма фалшиво прието състояние |
| Login, MFA, popup OAuth, blocked embedded OAuth | Проверен профил и flow; външен login не се отчита като CEF login без evidence |
| Upload/download, collision, cancel, прекъсване | Правилен файл/цел/профил, без тих overwrite или автоматично изпълнение |
| Release update и profile format mismatch | Съвместим комплект, безопасно спиране при несъвместимост, без мълчалив downgrade |
| Native Windows/Linux + macOS release | Самостоятелно evidence за всяка обявена платформа |

Go service/mailbox race checks, tagged adapter checks и native packaged
fixtures са отделни категории. `go test -race ./...` без desktop tags не
проверява целия adapter и не доказва C++/Objective-C ownership. CLI/TUI
builds и съществуващите GUI проверки трябва да останат работещи при
production интеграцията. Точните команди се фиксират в съответния task план.

Upstream issue за external pump/idle work е тестов риск, не възпроизведен
наш дефект: [CEF #4332](https://github.com/chromiumembedded/cef/issues/4332).

## Решения преди съответната implementation група

1. CEF release/API pin, macOS архитектури и минимални OS/toolchain версии.
2. Резултат от lifecycle spike-а и точен допустим Wails patch surface.
3. Profile ownership/defaults, restore политика и retention за artifacts/history.
4. UI layout, tab shortcuts, permissions UX и agent control/takeover политика.
5. Първи реален harness и transport за tools/annotations; доказани capabilities.
6. Go package/port layout, metadata schema и migrations, включително atomic
   transitions при profile deletion, grants и annotation delivery.
7. Лимити за tabs/profiles, snapshots, queues и resources; overflow semantics
   не могат да губят terminal резултати независимо от избраните числа.
8. Матрица на login доставчици, външни сайтове и frame/file сценарии, които
   ще се използват за acceptance; няма обещание за универсална web съвместимост.
9. Поддържани Windows/Linux targets и release/update/notarization процес.

Преди кодиране съответният етап получава конкретен plan, contracts,
task ownership и test commands, независим преглед и потвърждение на
неразрешените решения. Изготвянето на тази спецификация не започва тези етапи.

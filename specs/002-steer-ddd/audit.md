# Независим advisory audit: Steer DDD pilot

## Къде да се съсредоточи човешкият преглед

1. **Атомарна резервация и правилото за ревизия.** [internal/harnesses/codex/steer.go:103](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:103) и [internal/harnesses/opencode/steer.go:41](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/steer.go:41) държат проверката на текущите факти, `DecideCorrection` и резервацията в една mutex граница. Доставката следва след отключване. [internal/sessions/correction_rule.go:29](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/sessions/correction_rule.go:29) изисква пълно съвпадение на текущото задание, същата идентичност и точно +1, с отделно отхвърляне на overflow. Не установих race между проверка и резервация. Проверете дали пояснителните Reason текстове са достатъчни за бъдещата диагностика; процедурата правилно разклонява по Kind.
2. **Неясна доставка и закъснялата Codex разписка.** [internal/harnesses/codex/steer.go:25](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:25), [internal/harnesses/codex/steer.go:53](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:53) и [internal/harnesses/codex/steer.go:78](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:78) пазят pending при възможно изпращане и приемат само корелиран turn. Публикуването вече е явно и в нормалната процедура, и в late reconciliation. Новият тест реално наблюдава закъсняла разписка след отменено чакане. RPC транспортът е извън пилота; прегледах callback пътя му, но не направих изчерпателен транспортен одит.
3. **OpenCode accepted receipt след смяна на binding.** [internal/harnesses/opencode/steer.go:64](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/steer.go:64) проверява turn и очаквана ревизия отново, преди да придвижи assignment. [internal/harnesses/opencode/steer.go:18](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/steer.go:18) запазва deliveryUnknown след възможно изпращане и грешка. Това е запазено поведение, не нов общ transport skeleton. Човешкият преглед следва да държи разграничението между transport acceptance и действително прилагане от модела.

## Констатации, предложения и въпроси

### Доказани нововъведени дефекти/нарушения

Не установих нововъведен дефект или нарушение на одобрения Steer DDD договор в прегледаните 10 файла. Това не е доказателство за пълна коректност и не представлява решение за приемане.

### Съществуващо поведение за отделно обсъждане

**P2, предшестващо пилота: Codex не проверява request.Session при Steer.** В [internal/harnesses/codex/steer.go:103](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:103) адаптерните условия проверяват turn, pending и assignment, но не сравняват `request.Session` със `s.ref`. Заявка с чужд session handle и иначе съвпадащи turn/assignment факти може да бъде изпратена към nativeThread на receiver session. OpenCode проверява session handle на [internal/harnesses/opencode/steer.go:44](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/opencode/steer.go:44). Същото отсъствие се вижда в базовия Codex `session.go` преди извличането; не го приписвам на рефакторинга. Доказателството е статично, без допълнителна репродукция. Препоръка: отделно разрешена задача да уточни дали receiver session е единственият адресиращ авторитет; ако request.Session също е задължителна корелация, добавете проверка и fixture тест за чужд handle преди dispatch. Пилотът забранява unrelated fixes, затова тук не предлагам автоматична поправка.

### Предложения за покритие, без установен дефект

- **Нисък приоритет: разписката при нормален Codex успех.** [internal/harnesses/codex/steer.go:49](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer.go:49) извежда event publication от acceptSteer. Новият тест [internal/harnesses/codex/steer_test.go:47](/Users/martinandreev/Projects/open-source/conduit-go/conduit-go/internal/harnesses/codex/steer_test.go:47) доказва late event, а съществуващият интеграционен тест проверява върнатата нормална разписка. Полезен отделен assertion би наблюдавал точно една ControlReceiptEvent при нормален Steer и липса на такава при mismatch/native rejection. Не установих загубена или дублирана публикация в кода.
- **Нисък приоритет: branch coverage на преместването.** Новите тестове упражняват rule rejection, Codex cancel+late success, OpenCode несъвпадащ binding и unsent failure; съществуващите покриват OpenCode concurrency и uncertainty. Няма изрично ново покритие на Codex late native rejection и невалидна late разписка през реалния callback. Тези branch-ове са преместени без поведенческа промяна; fake RPC тестове биха направили бъдещи промени по-безопасни.

### Отворени въпроси

Няма липсващо съществено изискване, което да е било нужно да измисля за този пилот. При отделна бъдеща работа следва да се уточни session-handle политиката по горното съществуващо поведение. Разписките и в двата адаптера остават само транспортно доказателство, както изисква договорът.

## Поток и покритие

Codex влиза през Steer: проверка за предварителна отмяна → native inputs → reserveSteer под mutex → deliverSteer без mutex → явно обработване на rejection/unsent/possibly-sent → acceptSteer с turn корелация → ControlReceiptEvent. При отменено чакане след изпращане pending остава; RPC late callback изпълнява reconcileLateSteer и придвижва assignment само при валиден отговор. При невалиден отговор pending не се изчиства.

OpenCode: reserveSteer под mutex → native session.prompt без mutex → освобождаване на резервация и запазване на uncertainty при possibly-sent грешка → acceptSteer с повторна проверка на lease/revision → publication. Нито една процедура не доказва семантично прилагане на корекцията от модела. Файловете дават проследим локален поток без generic workflow, retry wrapper или спекулативен shared skeleton.

AssignmentRef и необходимите четири ID типа са преместени към sessions; provider ги реекспортира с type aliases. SameIdentity семантиката е запазена, unrelated ID типове не са местени. Няма промяна на schema или нови providers. Разликата в Codex от разрешено увеличение с пропуск до точно +1 и OpenCode overflow rejection е изрично одобрена от specs/002-steer-ddd/spec.md.

### Прегледан snapshot

Репозиториум: `/Users/martinandreev/Projects/open-source/conduit-go/conduit-go`. Одобрени документи: AGENTS.md, architecture.md, verification.md, specs/002-steer-ddd/spec.md, specs/001-harness-storage/harnesses.md. За 002 няма отделни plan/tasks/contracts/test-cases файлове; договорът и проверките са в spec.md. Независимият преглед следва implementation-audit skill; не получих предходна присъда или заключения на имплементатора.

Базата е `/tmp/conduit-steer-ddd-baseline/source.tar`, извлечена само в disposable `/tmp/conduit-steer-independent-baseline`. SHA-256 на архива: `01726568aed254b9f2ae47ccba9e61af33834497aa3ea6819ea952ad8ad6dfc7`.

Точен scope, в този ред: sessions/assignment.go, sessions/correction_rule.go, sessions/correction_rule_test.go, sessions/provider/ids.go, harnesses/codex/session.go, harnesses/codex/steer.go, harnesses/codex/steer_test.go, harnesses/opencode/session.go, harnesses/opencode/steer.go, harnesses/opencode/steer_test.go; всички с префикс internal/. Composite SHA-256 на изхода от `shasum -a 256` за тези десет относителни пътя в посочения ред: `907a520eb561a1f3d0aceab91b5ed4c9897fe40f6be49954346a12f6baafe784`. Хешът е проверен след тестовете и съответства на началните съдържания. Scope е некомитнат; широко git status показва отделни наследени промени от Go rewrite, които не са включени в този одит.

Прегледах още релевантния Codex RPC callback/dispatch път, OpenCode control.Prompt, session state guards, съществуващите fake/fixture тестове и CLI entry point. Не проверявах изчерпателно останалия rewrite, live providers, моделно поведение, production DB или Windows.

### Лично изпълнени проверки

Всички Go проверки използваха `GOCACHE=/tmp/conduit-go-cache TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache`.

| Проверка | Резултат |
| --- | --- |
| gofmt -l върху точните 10 файла | празен изход, форматът е коректен |
| go test -race ./internal/sessions/... ./internal/harnesses/... | успешно, част от резултатите cached |
| go test -race -count=1 ./internal/sessions/... ./internal/harnesses/... | успешно, принудително изпълнение, включително двата адаптера |
| go test ./... | неуспех само при link на internal/storage/turso.test: macOS27 SDK .tbd съдържа arm64e.x1, което системният linker не разпознава |
| CGO_ENABLED=0 go test ./... | успешно, cached резултати |
| CGO_ENABLED=0 go test -count=1 ./... | успешно, принудително изпълнение, включително disposable storage tests |
| go vet ./... | успешно |
| go build ./... | exit 0; предупреждение за недостъпен module stat cache извън sandbox |
| go run ./cmd/conduit --help | exit 0, очакван scaffold help |

CGO_ENABLED=0 е само локален environment override след конкретния SDK link failure; системната конфигурация не е променена. Пълната база не е изпълнявана независимо; preexisting класификацията на SDK проблема е подкрепена от одобрения spec, а текущият failure е възпроизведен лично. Всички тестове са локални fake/unit или временна storage среда, без live модели/network/production DB. Не съм редактирал код, тестове, repository документи или commits. Докладът е advisory; решението е на човешкия reviewer.

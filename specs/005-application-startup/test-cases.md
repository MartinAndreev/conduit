# Приемателни проверки

## Application и инфраструктура

| ID | Сценарий | Очакване |
| --- | --- | --- |
| S01 | Няма конфиг, два harness-а | Discovery преди publish; валиден v1, няма модели/тайни; ready |
| S02 | Няма harness-и | Валиден конфиг, ready_with_warnings, без provider session |
| S03 | Съществуващ валиден конфиг | Byte-for-byte запазен; актуално discovery отделно от initial observation |
| S04 | Невалиден/нечетим/бъдещ schema | Различими диагностики; blocked; оригиналът остава |
| S05 | Duplicate/unknown YAML keys | Няма тихо приемане/презаписване |
| S06 | Explicit executable липсва | Няма PATH fallback; конкретно предупреждение |
| S07 | Version timeout/error/неразпознат output | Bounded capture/cleanup; другият harness се проверява; version неизвестна |
| S08 | Отказ преди и по време на probe | Няма следващи ефекти; няма останал child process |
| S09 | Отказ след publish | Файлът остава валиден; следващ run го използва |
| S10 | Няма права/място, failure при publish | Няма частичен целеви файл; само собствените temp файлове се чистят |
| S11 | Два едновременни първи старта | Един цял валиден файл; загубилият валидира победителя, не го презаписва |
| S12 | Config се появява конкурентно и е невалиден | Blocked без replacement |
| S13 | Краен reread се провали | Честен outcome за вече публикувания файл; без фалшив ready |
| S14 | Бавен/затворен progress consumer | Application не блокира; terminal outcome остава достъпен |
| S15 | Retry след поправка | Нов operation ID, disk reread, никакви стари statuses |
| S16 | Help/version на CLI | Без директории, config writes, discovery или inference |
| S18 | Home path и project cwd | Конфиг само в `<user-home>/.conduit/config.yaml`; смяна на cwd не променя целта; неразрешим home дава грешка без fallback |
| S17 | Symlink/unsupported filesystem | Няма неявно писане в чужда цел или unsafe fallback |
| S19 | Cancel по време на lookup/неуспешен launch | Няма launch след наблюдаван cancel; cancellation и техническата причина остават различими |
| S20 | Root exit с descendants и пренасочени потоци | Cleanup се изпълнява и при успешен/неуспешен root exit |
| S21 | Kill failure и неприключващ Wait/reader | Caller връща в cleanup срока; няма ready/publish; outcome запазва observer и техническата причина |
| S22 | Повторен cleanup и конкурентно наблюдение | Един owner/worker; копирани snapshots; eventual reap след caller timeout се наблюдава без загуба на ownership |
| S23 | Stat failure, сменен файл и файл, изчезнал след Lstat | Запазена оригинална Stat причина; identity mismatch е отделен; късен ENOENT не става първоначално absent |
| S24 | Shared lock и probe descriptors | Error/panic се запазват, mutex се освобождава; schema IDs и probe описанията са съгласувани без implicit нов harness |

Deterministic fake ports тестват реда и отказите. Реални file adapters се проверяват в temp directories, probes с test helper executable; CI няма нужда от инсталирани Codex/OpenCode или акаунти. Тестовете не стартират истински агенти.

## Проверки при delivery на A

Фокусирани Go tests за новите packages; `go test ./...`, `go vet ./...`, `go build ./cmd/conduit`; smoke `go run ./cmd/conduit --help` и `--version`; race за новите startup/configuration/infra packages. Ако текущата Turso/native среда изисква CGO_ENABLED=0 за пълния suite, това се отчита отделно от race проверките; не се твърди непроведена проверка. Paths/permissions и create-if-absent се проверяват за macOS/Windows/Linux с наличния CI или се отбелязват непроверените платформи.

## Presentation след A

Еднакви fixtures за first run, existing config, no harness, blocked, cancelled, retry и slow probe. Проверки за keyboard navigation/focus, resize, видима причина, съвпадащ status и никакви UI-owned writes. Real TUI/GUI captures за startup/welcome; реалният GUI asset build работи без CDN. GUI application bridge не се счита доказан от browser screenshot.

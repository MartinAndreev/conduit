# Startup application contract — проект

## Граници и отговорности

Предложени owners, подлежащи на одобрение със спека:

- `internal/configuration`: schema/value types и чисти правила за конфигурация; `rules` подпака само за правилата на този owner.
- `internal/app/startup`: use-case процедура, query/step ports, immutable snapshots и резултат; без UI imports.
- `internal/startupinfra`: файлов repository, directory resolution, executable/version detection. Използва съществуващия `internal/process`, когато той покрива нужния bounded lifecycle; не копира process ownership логиката.
- Терминален и desktop composition roots инжектират едни и същи implementations. UI не чете конфигурационни файлове и не стартира probes директно.

Това са нови граници за ограничен startup slice, не разрешение за реорганизация на останалия код. `internal/app/persistence.go` запазва сегашната си роля. `Provider.Inspect` не е detector: текущият Codex implementation връща descriptor без executable probe.

## Местоположение

Одобрено от потребителя: глобалният конфиг е `<user-home>/.conduit/config.yaml`. Home се разрешава чрез `os.UserHomeDir()`, а пътят се сглобява platform-safe; `~` е документационна нотация, не буквално име на директория. Не използваме `os.UserConfigDir()`. При липсващ/неразрешим home връщаме диагностика, без fallback към текущата директория. Бъдещият `<project>/.conduit/config.yaml` е отделен scope и не се създава в този slice.

## Конфигурация v1 — семантичен schema

`schema_version: 1`; `harnesses` с разрешени ключове `codex` и `opencode`. За всеки: optional explicit executable override и optional `initial_discovery` с kind, path и parsed version, когато известни. Initial discovery е историческо наблюдение, никога доказателство за текуща готовност. `assignments` е празна map при създаване; runtime не избира роля или модел. Не пазим credentials, environment dumps, суров stdout/stderr или authentication данни.

Строга валидация: duplicate/unknown keys, грешни типове, непозната schema version и невалидни explicit paths са диагностични грешки. По-нова версия не се презаписва. Начално създаване не е миграция. Пълният future role/settings schema се добавя чрез отделен одобрен contract; старият TypeScript YAML не се конвертира неявно.

## Публична семантика

`Initialize` получава context за отказ и sink за startup snapshots; връща един краен outcome. Конкретните Go сигнатури се уточняват в application задачата без промяна на тези гаранции.

Snapshot: operation identity, монотонна revision, overall status, подредени step states, вече завършени harness observations, structured diagnostics, config path при наличност. Snapshot е пълен и не изисква UI да възстановява факти от текстов журнал. Стойностите са копия, не mutable state на процедурата.

Overall: `running`, `ready`, `ready_with_warnings`, `blocked`, `cancelled`. Step: `pending`, `running`, `completed`, `skipped`, `failed`, `cancelled`. Diagnostic: стабилен code, severity, owning step/harness и безопасни details. UI избира текст/икона/локализация по code. Reason е за човек, не условие за branching.

Progress transport: bounded in-process latest-snapshot mailbox; producer не чака UI rendering, старите междинни snapshots може да се слеят. Крайният outcome се връща отделно и не зависи от доставка на progress. При subscribe/presentation adaptation се доставя последният snapshot; няма обещание за пълен audit log. Затваряне/отказ и ownership на mailbox имат tests; не се ползва loss-sensitive harness broker без проверка на съвместимата семантика.

## Harness detection

Explicit executable path, ако е зададен, има предимство; иначе OS executable lookup. При невалиден explicit override няма скрит fallback към PATH. Lookup не изпълнява shell. Двата поддържани probes са отделни аргументни масиви за `--version`; точната поддръжка се проверява в официалните CLI docs/фиксирани fixtures преди имплементация.

Факти: `missing`, `found`, `probe_failed`, `probe_timeout`; parsed version може да е unknown. Намерен executable или успешно version output не означава supported session protocol, authenticated account или достъпен модел. Тези три capabilities остават unknown в този slice. Ограничен capture, context timeout и cleanup/reap на започнатия child process. Не отваряме provider session и не четем auth storage.

Version failure е warning, а не конфигурационна загуба. Отказ на потребителя прекратява цялата процедура. Не обещаваме sandbox забрана на мрежата за произволен executable: целта е да извикаме само документиран локален metadata режим на избрания инструмент.

## Четим flow

Procedure `Initialize`: query конфиг → rule за действие → query discovery → rule за initial config → step publish при нужда → query final config → rule за readiness → terminal snapshot/outcome.

Правилата връщат конкретни Kind/Reason решения за въпросите „можем ли да използваме конфигурацията“, „трябва ли да създадем файл“, „готов ли е startup“. Не извършват I/O. Step публикува един логически конфигурационен запис. Failure/reconciliation paths с няколко действия имат имена. Wrapper се използва само за реална повторяема механика; няма предварително измислен generic skeleton.

## Запис и конкурентност

Файловият adapter пише temp файл в целевата директория, затваря/flush-ва го и публикува целия файл чрез platform-safe create-if-absent. Обикновен rename с overwrite и check-then-write не удовлетворяват договора. При unsupported filesystem връща ясна грешка вместо unsafe fallback. Конкретният OS механизъм и гаранциите при power loss се документират и проверяват преди приемане на adapter-а.

Already-exists е отделен outcome: прочитаме и валидираме съществуващия файл, без да прилагаме собствени defaults върху него. Не следваме съществуващ config symlink за запис. Cleanup премахва само temp файловете, създадени от текущата операция. Грешка след успешното публикуване се отчита като непотвърден финален прочит, не като „нищо не е записано“.

## Retry и application lifetime

Няма автоматичен retry. Explicit Retry стартира нова операция и reread-ва диска. Една UI инстанция не стартира втори Initialize паралелно; cross-process безопасността остава в file adapter-а. Закриване на приложението cancel-ва неговата процедура и изчаква bounded cleanup. Не въвеждаме глобален mutex за целия lifecycle на Conduit.

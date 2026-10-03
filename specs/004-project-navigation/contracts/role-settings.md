# Настройки на роли — legacy инвентар и нов UI

Статус: проверен инвентар от стария TypeScript код; новата подредба е предложение за обсъждане, без имплементация.

## Проверени източници

В оригиналното репо `conduit`:

- `packages/conduit/src/domains/configuration/types/config.ts`: RoleConfig, Config и CONFIG_FILE = conduit.yml.
- `packages/conduit/src/domains/configuration/repositories/project-config.ts`: defaultConfig, serializeConfig и parseConfig.
- `packages/conduit/src/domains/configuration/types/settings.ts`: resolved settings и ConfigurationSource.
- `packages/conduit/src/domains/configuration/types/global-profile.ts`: глобален профил и versioned save input.
- `packages/conduit/src/domains/configuration/repositories/configuration-repository.ts`: разрешаване на ефективни стойности и произход.
- `packages/conduit/src/domains/roles/assets/role-templates.ts`: генерирани инструкции за архитект, researcher, implementation роли, QA, documentation и reviewer.

Това е статичен прочит на код, не изпълнен legacy UI тест или потвърден feature parity. Не променяме оригиналното репо.

## Налични настройки и предложено представяне

| Legacy поле | UI представяне |
| --- | --- |
| Role name, description, roleKind | Списък, описание и вид на ролята |
| runner | Harness |
| model | Модел от установения каталог |
| effort | Ниво на разсъждение, според harness/model capabilities |
| mode | Основен / подагент; старите primary/subagent не утвърждават Go execution механизъм |
| skill.source, skill.sha256 | Редактор/източник на инструкции и проверка на skill в разширените настройки |
| readOnly | Намерение за режим само за четене |
| owns | Обхват на работа; не е сам по себе си sandbox enforcement |
| dependsOn | Зависимости в разширените настройки; точният бъдещ scheduler договор е отделен |
| specsDir, stateDir, worktreeRoot | Таб „Проект“: местоположения |
| worktreeRetentionDays, runDiagnosticsRetentionDays | Таб „Проект“: срокове за съхранение |
| Global profile, effective settings, provenance | Явен обхват на редакция и източник на ефективната стойност |

Legacy има вградени, global-profile, project, role-guidance и CLI източници. Не дефинираме нова precedence политика само чрез макета. Legacy defaults за harness, пътища и срокове не се пренасят автоматично в Go.

## Предложен екран

Табове „Роли“, „Harness-и“ и „Проект“. Макетът показва „Роли“ с избран архитект. Основните полета са видими; зависимости/skill проверка са в разширени настройки. „Запази“ променя конфигурацията, не стартира модел. Непопълненият модел в примера илюстрира липсваща настройка; initial assignment/default policy остава отворена.

Таб „Harness-и“ използва [startup договора](startup-configuration.md): откриване и metadata проверка, отличени от реална inference проба. UI показва установено/непроверено/неуспешно, а не свежда инсталация, authentication и достъп до модели до едно зелено състояние.

Предложените критичен reviewer на spec и code reviewer са отделни употреби. Старият reviewer template е за integrated implementation review; не го представяме като съществуващ pessimistic spec reviewer. Ролите в макета не утвърждават окончателен default списък.

„Ограничени пътища“ е дизайнерско предложение. Преди реализация трябва да се договорят приложимостта и enforcement: unsupported ограничения се обозначават, не се обещава sandbox само въз основа на `owns`. Глобални/project overrides, файлът за новия Go конфиг и правила за редакция при активни задачи също изискват договор.

[Макет на настройките](../screens/06-role-settings.png). Илюстративни стойности, не capture на действащо приложение.

Допълнителни макети: [Harness-и](../screens/07-harness-settings.png) и [Проект](../screens/08-project-settings.png). Те визуализират metadata наблюдения и наследените категории настройки; не утвърждават автоматични модели, scope/precedence или cleanup defaults.

## Опционална проектна памет

В таб „Проект“ добавяме секция „Памет“ с включване на локален Engram от потребителя, отделен статус на готовността и видим път до данните. Изключен по подразбиране е предложената начална стойност. [Договорът за памет](project-memory.md#engram-като-потребителска-настройка) описва предложените enable/disable и failure сценарии. Секцията има отделен [макет за включване и подготовка](../screens/09-memory-setup.png); Engram се изтегля при включване.

# Turso каталог — прието storage решение

Статус: Turso вариантът е одобрен от потребителя на 2026-10-04 за application група A. Името на документа се запазва, за да останат валидни съществуващите препратки.

## Изолирана база

Използва се `~/.conduit/catalog.db`, с embedded Turso engine от съществуващата dependency и отделна catalog schema v1. Session базата и нейният `Store`/schema/generator остават непроменени: текущият adapter приема само session schema v1 и няма migration механизъм.

Catalog metadata има собствен application marker и schema version. `projects` пази стабилен ID, име и unique canonical path. `last_opened` пази отделен project ID за `tui` и `gui`, с foreign key към регистрацията. Unknown/future/corrupt schema се отказва без автоматична промяна; dangling preference има отделна диагностика.

Read при липсваща база връща празен каталог без създаване на файл. Първата регистрация създава schema и проекта в защитена транзакция. Два едновременни първи старта трябва да се проверят с отделни connections; `SetMaxOpenConns(1)` не сериализира различни приложения. Write reservation/transaction поведението се доказва върху временна Turso база преди adapter реализацията.

Регистрация и last-opened са отделни транзакции. Отказът на preference запазва успешната регистрация и се отчита като частичен успех. Foreign-key enforcement важи за всяка connection.

## Идентичност на директорията

`Abs`/`Clean`/`EvalSymlinks` сами не нормализират case aliases на всички filesystem-и. Unique текстов път без допълнително доказателство може да допусне дубликати; глобален lowercase може да слее различни директории.

Filesystem adapter-ът трябва да потвърждава реалния регистър/identity чрез OS факти. Конкретният алгоритъм се валидира за symlinks, case-sensitive/insensitive directories, Windows drive/UNC и permission откази. Няма приблизителен fallback или постоянен inode като project ID. Native платформените пропуски се отчитат.

## Owners

- `internal/projects`: стойности и чисти domain правила.
- `internal/app/projects`: процедури, ports и публични outcomes.
- `internal/projectinfra`: directory identity/availability и home resolution.
- `internal/storage/turso/catalog`: самостоятелен catalog store/schema и технически правила.

При използване на Jet generated файловете имат отделна destination; текущият session generator не се пренасочва към съществуващ output. Няма промени в UI или startup composition преди приемане на application групата.

## Установено ограничение — чака решение

Независимият audit възпроизведе race при външно изтриване на каталога между `Lstat` и отварянето на Turso connection: прочитът може да създаде празен файл и да върне грешка за schema. Обикновен прочит на първоначално липсващ каталог не създава нищо. `tursogo v0.7.2` не предоставя no-create/read-only open чрез използвания connector; допълнителен `Stat` не премахва race-а.

Това е неразрешено отклонение от гаранцията за прочит без създаване. Документирането му не означава приемане или отслабване на договора. Нужен е избор между отделна работа по driver възможността и изрично приемане на ограничението. Не се въвежда собствен driver или промяна на dependency без такова решение.

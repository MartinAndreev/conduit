# A — каталог и начално възстановяване

Статус: одобрена за implementation на 2026-10-04, с обща глобална Turso база `~/.conduit/catalog.db`.

Ownership: domain/application агент — `internal/projects` и `internal/app/projects`; filesystem агент — `internal/projectinfra`; storage агент — `internal/storage/turso/catalog`. Domain/application owner публикува и притежава ports/outcomes; другите адаптери се съобразяват с тях. Без едновременни редакции на shared contracts или промени по съществуващите session schema/генератор. Интерфейсите и composition roots са извън група A.

Прочети `spec.md`, `contracts/catalog.md` и `test-cases.md`. След одобрение задачата притежава само договорените domain/application/adapters и техните тестове; точните Go пакети се добавят към възлагането преди старт.

Реализирай регистрация на съществуваща директория, прочит на каталог, preference за последния отворен проект и проверено възстановяване. Чистите правила връщат специфични Kind/Reason решения; техническите причини се запазват. Не добавяй UI, автоматични миграции, project writes или изпълнение на агенти.

Провери K01–K13 с fake ports и временни filesystem/storage fixtures, приложимите race tests, пълен Go suite и vet. Няма тестове върху реални потребителски проекти. Без commit без изрична заявка.

# Проучване: заменяем backend за проектна памет

> Историческо сравнение: потребителят уточни, че паметта трябва да работи в служебната папка на клиента без отделно поддържана услуга. Shortlist по-долу не е текуща препоръка. Последващо решение: [опционален локален Engram](../contracts/project-memory.md#engram-като-потребителска-настройка).

Дата на проучването: 2026-10-03. Извършено от Sol подагент и прегледано от архитекта. Само документационно проучване по първични източници; няма инсталации, benchmark, inference или доказана интеграция. Препоръките не са одобрен избор на продукт.

## Граница

[Договорът за памет](../contracts/project-memory.md) оставя документите, решенията, одобренията и фактическата история при Conduit. Външният продукт е заменяем производен индекс. Извличане на нови спомени чрез LLM е отделна възможност, чиито резултати подлежат на преглед.

## Кандидати

| Продукт | Практическа интеграция | Силни страни и ограничения | Оценка |
| --- | --- | --- | --- |
| Qdrant OSS | Локален server, REST/gRPC, официален Go client | Payload за project/source/version, филтри, dense/sparse retrieval, update/delete и snapshots. Embeddings са отделна зависимост; не извлича сам факти от разговори. | Първи кандидат за тесен адаптер за търсене; допълнителна услуга към Go приложението. |
| Graphiti | Python service с REST/MCP и graph DB | Времеви факти, episodes като източници, semantic/BM25/graph retrieval. Изисква extraction LLM и embeddings; качеството на structured output влияе на ingestion. | Кандидат, ако връзките и промяната на факти във времето оправдаят сложността. |
| Mem0 OSS | Python/TypeScript библиотека или self-hosted server; Go през HTTP | Автоматично извличане на памет и semantic retrieval, конфигурируеми модели/embeddings. Source metadata и човешкият преглед остават наша отговорност. | Кандидат за предложения за памет; CRUD/export на конкретния server трябва да се проверят. |
| Letta Code | Stateful agent harness с избираем local backend | Управлява и самите агенти, memory blocks и lifecycle. Оригиналният Letta repo насочва активната разработка към Letta Code, а V1 server е в archive branch. | По-широка от нужната отговорност за memory адаптер; не е първи избор. |
| Chroma OSS | Persistent local/HTTP deployment | Collections, metadata и CRUD за semantic retrieval. Актуалните docs ограничават новия Search API до Cloud. | Алтернатива за прост индекс; не приемаме Cloud hybrid функционалност за локално налична. |

Прегледаните OSS repositories обозначават Apache-2.0. Това не покрива автоматично hosted услугите, graph DB зависимостите или лицензите на локалните модели. За конкретен PoC се фиксират версии и се проверяват техните лицензи.

### Първични източници

- Qdrant: [repository и Go client връзка](https://github.com/qdrant/qdrant), [hybrid queries](https://qdrant.tech/documentation/search/hybrid-queries/), [snapshots](https://qdrant.tech/documentation/operations/snapshots/). Описаният тук вариант е Server; Edge не е проверен като embedded Go решение.
- Graphiti: [core, зависимости и локални модели](https://github.com/getzep/graphiti), [MCP операции и групи](https://github.com/getzep/graphiti/blob/main/mcp_server/README.md). Graphiti OSS и Zep hosted са различни предложения. Namespace/group не се приема автоматично за граница на правата за достъп.
- Mem0: [OSS overview](https://docs.mem0.ai/open-source/overview), [конфигурация и ограничения](https://docs.mem0.ai/open-source/configuration), [repository](https://github.com/mem0ai/mem0). При проверката актуалната конфигурационна документация обозначава graph memory като Platform-only; не разчитаме на по-стари описания за OSS graph parity.
- Letta: [пренасочване на разработката](https://github.com/letta-ai/letta), [актуален Letta Code](https://github.com/letta-ai/letta-code). Пълна offline работа и точният интеграционен API не са проверени в изпълнение.
- Chroma: [repository](https://github.com/chroma-core/chroma), [локален server](https://docs.trychroma.com/production/chroma-server/client-server-mode), [CRUD](https://docs.trychroma.com/reference/js-collection), [Search API availability](https://docs.trychroma.com/cloud/search-api/overview).

## Поддръжка — ограничени наблюдения

Проучването видя release записи: Qdrant v1.19.1 (04 Sep), Graphiti v0.30.2 (08 Sep), Mem0 ts-v3.3.1 (25 Sep) и v2.2.0 (23 Sep), Letta Code v0.34.1 (30 Sep), Chroma 1.5.9 (05 May). GitHub изгледът не показа година за тези записи; това не доказва най-нов release към датата на проучването. Преди избор трябва да се проверят пълни дати, поддържани версии и compatibility.

Източници: [Qdrant releases](https://github.com/qdrant/qdrant/releases), [Graphiti releases](https://github.com/getzep/graphiti/releases), [Mem0 releases](https://github.com/mem0ai/mem0/releases), [Letta Code releases](https://github.com/letta-ai/letta-code/releases), [Chroma releases](https://github.com/chroma-core/chroma/releases).

## Предложение за следваща проверка

Първо измерваме прост локален baseline с изрично свързани документи и търсене. Qdrant заслужава сравнение за semantic retrieval; Graphiti — само ако въпроси като „кое решение е заменило това и защо“ имат измерима полза от graph retrieval спрямо нашите явни връзки. Mem0 може да се оцени отделно за извличане на предложения, а не за самостоятелно одобрение на факти.

Еднакъв синтетичен корпус: два проекта с еднакви термини, заменено решение, противоречащо наблюдение, променен source и изтрит запис. Проверки: project isolation, правилни sources/versions, повторно индексиране без дубликати, delete propagation, export/rebuild, restart/outage и работа без outbound traffic при offline режим. Измерваме retrieval качество, пропуски, латентност, ресурси и всички LLM/embedding разходи. Backend timeout трябва да оставя явно ограничение, а не измислен контекст.

Неизяснени: допустима допълнителна услуга към desktop/TUI, модел за embeddings, нужда от extraction, корпус/метрики и ресурсни лимити. Няма започнат PoC или избран backend.

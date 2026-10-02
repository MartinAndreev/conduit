# Независим консултативен преглед на Steer correction

## Къде да се съсредоточи човешкият преглед

1. `internal/harnesses/codex/steer.go:116–153`: класификацията на незабавна и късна грешка. Native rejection и доказано неизпращане освобождават резервацията; възможно изпращане я запазва. Късният rejection освобождава само съвпадащ RequestID и публикува предупреждение след отключване. Последователността и ProviderError полетата са запазени спрямо before snapshot.
2. `internal/harnesses/opencode/steer.go:59–94`: след native receipt binding се проверява отново преди промяна на assignment; транспортна несигурност задава deliveryUnknown и блокира повторно изпращане. Native rejection е отделна именувана подпроцедура. Предишното поведение при променен binding (RemoteEffectAccepted, без assignment advance) остава същото.
3. `internal/harnesses/internal/lockscope/mutex.go:10`: wrapper-ът притежава само Lock/defer Unlock/синхронно изпълнение; няма retry или recovery. Callback-ите за reserve и accept държат проверката и промяната в една критична секция. Няма директни Lock/Unlock в двата steer.go; доставка, Now и emit са извън wrapper-а. Синхронността е договор към caller-а, а не статично гарантирана от Go; всички текущи callback-и го спазват.

## Находки и препоръки

Не установих нов дефект или нарушение на одобрената correction рамка. Това не доказва обща коректност и не е решение за приемане.

- Подобрение с нисък риск: новият Codex failure matrix проверява подпроцедурата директно, а не входа Steer. Полезен бъдещ регресионен тест би изпратил native rejection през scripted RPC и би проверил последващ разрешен опит и пълния ProviderError. Настоящото свързване Steer → handleSteerDeliveryFailure е видимо и непроменящо поведението; това е ограничение на доказателството, не установен дефект.
- Предшестваща особеност, извън корекцията: Codex reserveSteer не проверява request.Session, за разлика от OpenCode. Това е идентично в before snapshot и assignment изрично забранява промяна на session-handle policy. Не предлагам поправка като част от този пакет.
- Предшестваща особеност: Codex незабавно освобождаване е без RequestID guard; късното е с guard. Извличането не променя това. В разгледаните текущи потоци pending reservation блокира конкуриращ Steer, затова не установих възпроизводим нов race от корекцията.

## Поток, покритие и граници

Steer резервира върху текущите adapter факти и чистото domain решение под един mutex; доставя извън него; избира именуван failure/rejection път или accept; публикува receipt след отключване. Receipt означава транспортно приемане. Codex cancellation след изпращане оставя pending и късен корелиран отговор може да приеме assignment; OpenCode маркира несигурността и блокира нов dispatch.

Лично изпълнени проверки (GOCACHE=/tmp/conduit-go-cache, TURSO_GO_CACHE_DIR=/tmp/conduit-turso-cache):

- `go test -race -count=1 ./internal/harnesses/codex ./internal/harnesses/opencode ./internal/harnesses/internal/lockscope ./internal/sessions` — PASS, без използване на test result cache.
- Същите focused race packages без `-count=1` — PASS (cached; по-слабо доказателство).
- `go vet ./internal/harnesses/codex ./internal/harnesses/opencode ./internal/harnesses/internal/lockscope ./internal/sessions` — PASS.
- `gofmt -l` за шестте scope файла — празен резултат.

Прегледани са wrapper success/error/panic tests (точно едно изпълнение, идентична грешка/panic и отключване), failure cause/code/effect/retry assertions, concurrent dispatch, stale/rule rejection, canceled uncertain delivery, късно receipt reconciliation и mismatched receipt тестове. Допълнително са прочетени RPC call, adaptererror, OpenCode Control, session emit и нужните state callers. Не са правени live model/network calls, full storage suite, build или CLI help; тези проверки остават отговорност на интегрираната верификация. Не са четени старите audit/walkthrough заключения. Кодът и тестовете не са променяни.

Прочетени договори: AGENTS.md, routed architecture/verification, implementation-audit skill и specs/002-steer-ddd/spec.md, включително Human review correction. Пакетът съдържа само spec, audit и walkthrough; няма отделни plan/tasks/test-cases/contracts за този пилот.

## Snapshot

Работна директория: `/Users/martinandreev/Projects/open-source/conduit-go/conduit-go`; HEAD `5d6615e8319b0d069503a38e14ae86f73fd3d1ea`; Go файловете са untracked в миграционната работна директория. Ограничението е шестте посочени correction файла. Before е извлечен само в `/tmp/conduit-steer-correction/reviewer-before`.

SHA-256:

```
before.tar bdfba5da760d012d6024277323dc0288f3c89f51a72cbe4bd855590a0b967348
codex/steer.go f1bab7e9f67ae902de74c8e270483440815397b5c192c1be0719be6e331460d6
codex/steer_test.go 7a52037f6b17f37d352f0c6f3e0e632c6999019a8c4fcfbdb4f8db023ee5b802
opencode/steer.go 99c347765a7c7e83e8336e5909fdee7494cfe6ca510430fe48884ca7bdc1e123
opencode/steer_test.go 4872f00ad18670cb0878bf4be99e6b84ae3b439c9e677a90b96c63c38a252601
lockscope/mutex.go b9eca7d9c038d0e57f57098b2e88b0b78f2dbc3cdf37c2742e7fd75c97152b9e
lockscope/mutex_test.go 1a6e1482b30e0e7edd6dd40e6111381795288e63f0420bfc56eb52ef9a2b2070
```

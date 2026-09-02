# Архитектура tripgo-infra

Статус: актуализирован после этапа 8 (2026-09-01).

## Границы продуктов

```text
cmd/tripgoctl
  -> internal/cli
  -> internal/cluster, internal/lab
  -> internal/generator, internal/pushimage, internal/pushartifact
  -> internal/buildinfo, templates

cmd/push-service
  -> internal/pushservice
  -> internal/buildinfo
```

Запрещены импорты:

- `internal/cli` → `internal/pushservice`;
- `internal/pushservice` → `internal/cli`.

Ограничение продублировано в `.golangci.yml` через `depguard`. Общий пакет
`internal/buildinfo` содержит только технические build metadata. Новые shared
пакеты добавляются только когда у обоих продуктов появляется реальная одинаковая
потребность; заранее общие abstractions не создаются.

## CLI wiring

`cmd/tripgoctl` отвечает только за process boundary: stdout/stderr, cwd provider,
build metadata, exit. `internal/cli.NewRootCommand` получает эти capabilities
через `Dependencies`; пакет не вызывает `os.Getwd` и тестируется без изменения
глобального cwd.

Command tree реализован на Cobra v1.10.2. Работают `version`, полный
`cluster start/status/stop`, lifecycle `environment start/status/stop/reset/list`
для labs 1–5, component-aware `environment logs` и `connect`. Default completion
command отключена, потому что она не входит в публичный контракт v1.

## Configuration boundary и каталог

`internal/environment` — единственная граница декодирования
`environment.toml`. Parser на `go-toml/v2` запрещает неизвестные поля, затем
проверяет schema/cross-field constraints и нормализует components в стабильный
порядок. Ошибки представлены типом `ConfigError` с path, field, expected/actual
и TOML position, когда она доступна.

`internal/catalog` не читает файлы и не зависит от cwd. Он содержит immutable
components по labs 1–5, namespace, image manifest digests, port mappings и
полные накопительные env defaults. Возвращаемые slices копируются. Каталог
потребляют parser, generator и CLI; обратной зависимости от CLI нет.
Подробные pins и формулы: [`catalog-v1.md`](catalog-v1.md).

Отдельная команда config validation намеренно не добавляется: lifecycle-команды
будут вызывать parser до любых мутаций.

## Generator

`internal/generator` зависит от validated `internal/environment`, immutable
`internal/catalog` и embedded `templates.Files`, но не от Kubernetes client или
cwd. Результат — ordered in-memory bundle manifests + lock; отдельный writer
безопасно устанавливает его в `.tripgo`. Таким образом generation/golden tests
не требуют Docker или cluster.

До локального build/push Push image labs 3–5 требуют trusted internal digest
option из managed registry; пользовательский config не может задавать image.
Topics представлены desired-state ConfigMap, а не одноразовым Job. Полный контракт:
[`generator-v1.md`](generator-v1.md).

## Cluster application layer

`internal/cluster` владеет Docker/kind process boundary, verified helper cache,
kind config, managed local registry, identity marker, state и operation lock.
CLI зависит от узкого `ClusterLifecycle` interface; unit-тесты command UX не
запускают Docker. Cluster package не импортирует CLI и возвращает typed
prerequisite/conflict errors для публичных exit codes 3/4.

Kind config генерируется программно из port catalog и не зависит от cwd.
Registry и control-plane имеют один random identity, записанный независимо в
registry labels, marker внутри kind node и atomic state. Подробности и
integration evidence: [`cluster-lifecycle-v1.md`](cluster-lifecycle-v1.md).

## Push Service wiring

`cmd/push-service` имеет независимый entrypoint и `-version`; application
boundary находится в `internal/pushservice.App`. Реализованы HTTP push/health и
admin behaviour API, gRPC Push/health/reflection, общий rate limiter,
context-aware latency/failures, structured logs и graceful shutdown.

Canonical runtime Dockerfile находится в
`internal/pushartifact/assets/Dockerfile`, использует `scratch` и numeric uid/gid
`65532:65532`. Двухфазная release-сборка сначала создаёт Linux Push binary для
amd64/arm64, затем встраивает Dockerfile и architecture-matched ELF в каждый
Darwin/Linux CLI. `internal/pushimage` проверяет ELF architecture, строит image
из extracted assets, пушит только в managed
`localhost:5001/tripgo-push-service` и принимает только immutable RepoDigest.
Generator получает эту ссылку через trusted options; remote OCI publication
пути нет.

## Embedded assets

`templates.Files` использует `go:embed` для namespace, PostgreSQL,
observability, Push, Redpanda и topics templates. Golden tests labs 1–5
проверяют, что assets попадают в бинарник без runtime filesystem dependency.

## Contracts

`third_party/homework` — git submodule. `scripts/sync-contracts` поддерживает два
режима:

```bash
./scripts/sync-contracts sync
./scripts/sync-contracts check
```

Relative symlink находятся в `api/` и указывают прямо на canonical файлы
submodule; fallback copies запрещены. `api/source.json` фиксирует repository,
submodule commit, точные link targets и SHA-256 canonical content. Check
проверяет links и metadata без сети. Поэтому submodule обязателен для build;
Windows, где symlink создаёт дополнительный portability/privilege contract, не
входит в поддерживаемые v1 платформы. Go embed не следует symlink, поэтому
release recipes после check staging-ят verified bytes в ignored
`internal/contractasset/generated`, собирают CLI с tag `embedded_contracts`,
проверяют exact OpenAPI/proto/manifest во всех четырёх binaries и удаляют staging.

## Версионирование

Оба бинарника получают одинаковые значения через linker flags:

- semantic version;
- source commit;
- UTC build timestamp.

Имя продукта задаётся entrypoint и различается. Dev defaults:
`dev/unknown/unknown`. Release-сборка отклоняет defaults и требует полный
`COMMIT`, равный committed superproject `HEAD`.

## Автоматизация

`Makefile` является единственной automation boundary текущей итерации:

- `make test`, `make test-race`, `make vet`, `make lint`;
- `make cross-build`;
- `make container-build`;
- `make contract-check`;
- `make verify`;
- destructive `make stage5-smoke`, `make stage6-smoke`, `make stage7-smoke`,
  `make stage8-smoke` и `make stage9-smoke` для завершённых вертикальных срезов.

Linux/Darwin workflow определён в `.github/workflows/release-smoke-platforms.yml`,
но в текущем непрокоммиченном состоянии он ещё не запускался и не является
свидетельством прохождения CI. `make verify` остаётся локальным gate; native
Linux и чистая внешняя машина остаются обязательными внешними release gates.

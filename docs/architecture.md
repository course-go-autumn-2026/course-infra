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

Запрет проверяется через `depguard` в `.golangci.yml`. Общий пакет
`internal/buildinfo` содержит только метаданные сборки. Новые общие пакеты
добавляются, только когда обоим продуктам нужно решить одну и ту же техническую
задачу; абстракции заранее не создаются.

## CLI wiring

`cmd/tripgoctl` отвечает за работу с процессом: stdout/stderr, функцию получения
cwd, метаданные сборки и завершение. `internal/cli.NewRootCommand` получает эти
зависимости через `Dependencies`; пакет не вызывает `os.Getwd` и тестируется
без изменения глобального cwd.

Дерево команд построено на Cobra v1.10.2. Работают `version`, полный набор
`cluster start/status/stop` и `environment start/status/stop/reset/list` для
работ 1–5, `environment logs` с выбором компонента и `connect`. Стандартная
команда автодополнения отключена: она не входит в публичный контракт v1.

## Configuration boundary и каталог

Только `internal/environment` декодирует `environment.toml`. Парсер на
`go-toml/v2` запрещает неизвестные поля, проверяет схему и ограничения между
полями, затем приводит компоненты к стабильному порядку. Ошибка `ConfigError`
содержит путь, поле, ожидаемое и фактическое значения, а также позицию в TOML,
если она доступна.

`internal/catalog` не читает файлы и не зависит от cwd. Он содержит неизменяемые
компоненты для работ 1–5, namespace, digest манифестов образов, пробросы портов
и полные накопительные наборы env по умолчанию. Возвращаемые срезы копируются.
Каталог используют парсер, генератор и CLI; сам каталог от CLI не зависит.
Закреплённые версии и формулы: [`catalog-v1.md`](catalog-v1.md).

Отдельная команда проверки конфигурации не добавляется: команды жизненного
цикла будут вызывать парсер до любых изменений.

## Generator

`internal/generator` зависит от проверенной конфигурации `internal/environment`,
неизменяемого `internal/catalog` и встроенных `templates.Files`, но не от клиента
Kubernetes или cwd. Он возвращает упорядоченный набор манифестов и lock в памяти;
отдельный модуль безопасно записывает их в `.tripgo`. Тестам генерации и сравнения
с эталонами не нужны Docker или кластер.

Для работ 3–5 нужен digest локально собранного и отправленного в управляемый
registry образа Push. Он передаётся через доверенную внутреннюю настройку;
пользователь не может задать образ в конфигурации. Желаемое состояние топиков
хранится в ConfigMap, а не в одноразовом Job. Полный контракт:
[`generator-v1.md`](generator-v1.md).

## Cluster application layer

`internal/cluster` управляет процессами Docker/kind, кешем проверенного
вспомогательного бинарника, конфигурацией kind, локальным registry, маркером
identity, состоянием и блокировкой операций. CLI зависит от узкого интерфейса
`ClusterLifecycle`; unit-тесты команд не запускают Docker. Пакет кластера не
импортирует CLI. Для ошибок предусловий и конфликтов он возвращает отдельные
типы ошибок с публичными кодами завершения 3/4.

Конфигурация kind генерируется из каталога портов и не зависит от cwd.
Registry и control-plane используют один случайный identity, независимо
записанный в метках registry, маркере внутри узла kind и атомарно сохраняемом
состоянии. Подробности и результаты интеграций:
[`cluster-lifecycle-v1.md`](cluster-lifecycle-v1.md).

## Push Service wiring

У `cmd/push-service` отдельная точка входа и `-version`; логика приложения
находится в `internal/pushservice.App`. Поддерживаются HTTP push/health и
admin behaviour API, gRPC Push/health/reflection, общий ограничитель частоты
запросов, задержки и ошибки с учётом context, структурированные логи и корректное
завершение работы.

Канонический Dockerfile для запуска находится в
`internal/pushartifact/assets/Dockerfile` и использует `scratch` и числовые uid/gid
`65532:65532`. Релиз собирается в два этапа: сначала Linux-бинарник Push для
amd64/arm64, затем Dockerfile и ELF нужной архитектуры встраиваются в каждый
Darwin/Linux CLI. `internal/pushimage` проверяет архитектуру ELF, собирает образ
из извлечённых файлов, отправляет его только в управляемый
`localhost:5001/tripgo-push-service` и принимает только неизменяемый RepoDigest.
Генератор получает эту ссылку через доверенные настройки. Публикации во внешнем
OCI registry нет.

## Embedded assets

`templates.Files` использует `go:embed` для шаблонов namespace, PostgreSQL,
observability, Push, Redpanda и топиков. Golden-тесты работ 1–5 проверяют, что
шаблоны попадают в бинарник и не требуют внешних файлов во время работы.

## Contracts

`third_party/homework` — закреплённый git submodule публичного
`https://github.com/course-go-autumn-2026/course.git`; контракты находятся в
`homework/contracts/`. `scripts/sync-contracts` поддерживает development-режимы:

```bash
./scripts/sync-contracts sync
./scripts/sync-contracts check
```

Относительные символические ссылки в `api/` указывают прямо на канонические
файлы submodule; запасные копии запрещены. `api/source.json` фиксирует репозиторий,
commit submodule, точные цели ссылок и SHA-256 содержимого. Проверка ссылок и
метаданных не требует сети. Submodule обязателен для сборки. Windows не входит
в список поддерживаемых платформ v1: символические ссылки в ней требуют
отдельного решения вопросов переносимости и прав доступа.

Go embed не следует символическим ссылкам. После проверки релизная сборка
помещает проверенные байты в игнорируемый Git каталог
`internal/contractasset/generated`, собирает CLI с tag `embedded_contracts`,
проверяет точное содержимое OpenAPI/proto/manifest во всех четырёх бинарниках
и удаляет временный каталог.

## Версионирование

Оба бинарника получают одинаковые значения через флаги линковщика:

- версию релиза (`main-<полный SHA>` в CI);
- commit исходников;
- время сборки в UTC.

Имя продукта задаётся в точке входа и различается. Значения по умолчанию для
разработки: `dev/unknown/unknown`. Релизная сборка отклоняет их и требует полный
`COMMIT`, равный закоммиченному `HEAD` основного репозитория.

## Автоматизация

`Makefile` содержит локальные команды сборки и проверки:

- `make source-check` проверяет исходники, гонки, фикстуры и контракты, не меняя
  состояние Docker/kind;
- `make lint`, `make cross-build`, `make container-build`;
- `make verify` выполняет полную локальную проверку с линтером и сборкой Docker-образов;
- `make integration-lifecycle`, `make integration-release-runtime` и
  `make integration-isolation` изменяют локальное состояние: проверяют жизненный
  цикл, работу финального бинарника и изоляцию нескольких окружений.

Workflow `.github/workflows/release-platform-check.yml` проверяет PR и публикует
проверенные сборки `main` в GitHub Releases. Один комплект архивов проходит
проверку установки на Linux/macOS amd64/arm64 и Docker/kind на Linux обеих
архитектур, затем публикуется без пересборки. На hosted macOS проверяются только
установка и запуск CLI; полный Docker-прогон перед первой публикацией требует
отдельной машины.
Подробности и ограничения: [`release-v1.md`](release-v1.md).

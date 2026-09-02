# tripgo-infra

Локальная инфраструктура лабораторных работ TripGo.

Монорепозиторий разрабатывает два независимых продукта:

- `tripgoctl` — CLI для управления локальным kind-кластером и окружениями;
- `push-service` — учебная заглушка, Linux-бинарник которой встраивается в
  release `tripgoctl` вместе с runtime Dockerfile.

Remote OCI Push Service не публикуется: CLI строит image локально и помещает его
в управляемый registry на `127.0.0.1:5001`. Release CLI также self-contained:
canonical OpenAPI/proto и их SHA manifest встраиваются из verified build staging.

Актуальные документы:

- [Design v1](docs/design-v1.md)
- [CLI contract v1](docs/cli-contract-v1.md)
- [Архитектура bootstrap](docs/architecture.md)
- [Встроенный каталог v1](docs/catalog-v1.md)
- [Детерминированный generator v1](docs/generator-v1.md)
- [Cluster lifecycle v1](docs/cluster-lifecycle-v1.md)
- [Environment lifecycle v1: labs 1–5](docs/environment-lifecycle-v1.md)
- [Local release pipeline v1](docs/release-v1.md)
- [Схема `environment.toml`](schemas/environment-v1.schema.json)

Канонические контракты курса подключены как обязательный git submodule в
[`third_party/homework`](third_party/homework); файлы в `api/` являются relative
symlink на canonical source, а не копиями. Поддерживаемые Darwin/Linux platforms
сохраняют links; Windows не входит в v1.

```bash
git clone --recurse-submodules git@github.com:course-go-autumn-2026/tripgo-infra.git
```

## Поддерживаемые платформы и требования

`tripgoctl` поддерживает macOS и Linux на `amd64`/`arm64`. Windows не входит в
v1. Для работы CLI нужен запущенный Docker daemon; kind и Kubernetes node image
загружаются и проверяются самим CLI.

Для сборки из исходников нужны Go 1.24+, Git с инициализированным submodule и
Python 3. Дополнительные локальные интеграции используют `curl`, `jq`, `psql`,
`goose` и `shasum`. Container/release gates требуют Docker Buildx.

## Разработка и проверки

Установите закреплённые protobuf tools, затем запустите безопасный source gate.
Он не создаёт Docker-контейнеры или kind-кластер и допускает dirty worktree:

```bash
make proto-tools-install
make source-check
```

`source-check` включает форматирование, `go vet`, race tests, fixture tests,
проверку protobuf/контрактов и отсутствие временного embedded staging. Линтер
устанавливается и запускается отдельно:

```bash
make lint-install
make lint
```

Полный локальный gate дополнительно выполняет cross-build и container build,
поэтому требует запущенный Docker daemon:

```bash
make verify
```

### Деструктивные интеграции

Каждая команда ниже изменяет Docker/kind state и отказывается запускаться, если
уже существует `tripgo-local`. Перед запуском убедитесь, что управляемые cluster
и registry не нужны другому процессу:

```bash
make integration-lifecycle       # lab 1: SSA, migrations, persistence и reset
make integration-release-runtime # lab 3: embedded Push image, registry и API
make integration-isolation       # parallel labs 2/4/5 и isolation/recovery
```

`integration-isolation` — тяжёлый gate: ему нужны не менее 10 GiB памяти Docker
и 15 GiB свободного места. Проверки имеют ownership-aware cleanup, однако после
аварийного завершения перед повторным запуском следует проверить `docker ps -a`.

## Release

Release-пайплайн локальный и ничего не публикует. Он требует clean committed
checkout, pinned submodule и полный commit hash. Воспроизводимость проверяется
двумя последовательными сборками:

```bash
make release-repro-check VERSION=v1.0.0 COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

После сборки финальный native archive проверяется через Docker/kind командой
`make release-platform-check`. В CI этот gate выполняется отдельно на macOS и
Linux для `amd64`/`arm64`; публикация артефактов workflow не производится.
Подробности и cleanup-процедура описаны в [release-документе](docs/release-v1.md).

## Установка CLI

`make install` собирает из текущего checkout полноценный self-contained CLI со
встроенными Push Service и контрактами, затем устанавливает его локально:

```bash
make install
tripgoctl version
```

По умолчанию используется доступный для записи `/usr/local/bin`, иначе
`$HOME/.local/bin`. Целевой каталог и build metadata можно задать явно:

```bash
make install INSTALL_DIR="$HOME/.local/bin" VERSION=dev
```

Команда не использует `sudo` и не изменяет shell profiles. Если выбранного
каталога нет в `PATH`, она печатает точную команду `export PATH=...`.

Для установки уже опубликованного release без сборки используется отдельный
проверяющий checksum скрипт:

```bash
./scripts/install-tripgoctl v1.0.0
```

Локальная облегчённая development-сборка CLI без release payload:

```bash
make build-tripgoctl
./build/bin/tripgoctl --help
./build/bin/tripgoctl version
```

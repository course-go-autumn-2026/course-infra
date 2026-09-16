# course-infra

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
- [Release pipeline v1](docs/release-v1.md)
- [Схема `environment.toml`](schemas/environment-v1.schema.json)

Канонические контракты из публичного [course](https://github.com/course-go-autumn-2026/course)
подключены как закреплённый git submodule в [`third_party/homework`](third_party/homework).
Файлы в `api/` — relative symlink на его `homework/contracts/`, а не копии.
Поддерживаемые Darwin/Linux platforms сохраняют links; Windows не входит в v1.

```bash
git clone --recurse-submodules https://github.com/course-go-autumn-2026/course-infra.git
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

На `push` в `main` GitHub Actions запускает проверки, собирает четыре архива
и после native-проверок публикует GitHub Release `main-<полный commit SHA>`.
`latest` — последняя опубликованная сборка `main`, не отдельный стабильный канал.
PR проходят те же проверки без публикации; CI-артефакты хранятся 14 дней.

Локальная команда ничего не публикует. Она требует clean committed checkout,
pinned submodule и полный commit hash. Воспроизводимость проверяется двумя сборками:

```bash
make release-repro-check VERSION=v1.0.0 COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

CI проверяет установку и запуск того же архива на macOS/Linux `amd64`/`arm64`,
а на Linux дополнительно выполняет Docker/kind gate `make release-platform-check`.
Полную Docker-проверку macOS нужно выполнить на машине с Docker перед первым
публичным релизом: hosted macOS arm64 runner не поддерживает nested virtualization.
Публикуются проверенные архивы без пересборки. Подробности, порядок первого запуска
и cleanup-процедура описаны в [release-документе](docs/release-v1.md).

## Установка CLI

После открытия репозитория и первой успешной публикации:

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL \
  https://github.com/course-go-autumn-2026/course-infra/releases/latest/download/install-tripgoctl \
  | sh
tripgoctl version
tripgoctl doctor
```

Установка не требует Go, Git или GitHub-токена. Нужны `curl`, `tar` и
`sha256sum` либо `shasum`; для работы инфраструктуры — запущенный Docker.
Установщик один раз определяет tag `latest`, затем скачивает архив и его checksum
из одной версии. Он не вызывает `sudo` и не изменяет shell profiles.
По умолчанию выбирается доступный для записи `/usr/local/bin`, иначе `$HOME/.local/bin`.
Если каталог отсутствует в `PATH`, установщик печатает команду для его добавления.

Можно сначала скачать и прочитать скрипт, а затем запустить:

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL \
  https://github.com/course-go-autumn-2026/course-infra/releases/latest/download/install-tripgoctl \
  -o install-tripgoctl
less install-tripgoctl
sh install-tripgoctl --install-dir "$HOME/.local/bin"
```

Для закрепления версии или отката передайте нужный tag из GitHub Releases
последним аргументом. Повторный запуск обновляет CLI; ошибка скачивания или
checksum не заменяет установленный бинарник. SHA-256 проверяет целостность архива,
но не защищает от компрометации самого репозитория или скрипта.

### Сборка из исходников

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

Локальная облегчённая development-сборка CLI без release payload:

```bash
make build-tripgoctl
./build/bin/tripgoctl --help
./build/bin/tripgoctl version
```

## Лицензия

[MIT](LICENSE). Файл лицензии также включён в release-архивы.
Внешние зависимости сохраняют собственные условия лицензирования.

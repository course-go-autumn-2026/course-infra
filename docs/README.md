# Документация course-infra

Локальная инфраструктура лабораторных работ TripGo.

В репозитории два независимых продукта:

- `tripgoctl` управляет локальным kind-кластером и окружениями;
- `push-service` служит учебной заглушкой. Его Linux-бинарник и Dockerfile для
  запуска встраиваются в релиз `tripgoctl`.

Образ Push Service не публикуется во внешнем OCI registry: CLI собирает его
локально и помещает в управляемый registry на `127.0.0.1:5001`. Релиз CLI также
содержит канонические OpenAPI/proto и манифест их SHA. Перед встраиванием эти
файлы проверяются во временном каталоге сборки.

## Разделы документации

- [Проектирование v1](design-v1.md)
- [Контракт CLI v1](cli-contract-v1.md)
- [Архитектура](architecture.md)
- [Встроенный каталог v1](catalog-v1.md)
- [Детерминированный генератор v1](generator-v1.md)
- [Жизненный цикл кластера v1](cluster-lifecycle-v1.md)
- [Жизненный цикл окружений v1: работы 1–5](environment-lifecycle-v1.md)
- [Сборка и публикация релизов v1](release-v1.md)
- [Схема `environment.toml`](../schemas/environment-v1.schema.json)

Канонические контракты из публичного [course](https://github.com/course-go-autumn-2026/course)
подключены как закреплённый git submodule в [`third_party/homework`](../third_party/homework).
Файлы в `api/` служат относительными символическими ссылками на его
`homework/contracts/`; копий контрактов здесь нет. Поддерживаемые платформы
Darwin/Linux сохраняют эти ссылки. Windows не входит в v1.

```bash
git clone --recurse-submodules https://github.com/course-go-autumn-2026/course-infra.git
```

## Поддерживаемые платформы и требования

`tripgoctl` поддерживает macOS и Linux на `amd64`/`arm64`. Windows не входит в
v1. Для работы CLI нужен запущенный Docker daemon; kind и образ узла Kubernetes
CLI скачивает и проверяет сам.

Для сборки из исходников нужны Go 1.24+, Git с инициализированным submodule и
Python 3. Дополнительные локальные интеграции используют `curl`, `jq`, `psql`,
`goose` и `shasum`. Проверки контейнерной и релизной сборки требуют Docker Buildx.

## Разработка и проверки

Установите закреплённые версии инструментов protobuf, затем проверьте исходники.
Эта проверка не создаёт Docker-контейнеры или kind-кластер и допускает
незакоммиченные изменения:

```bash
make proto-tools-install
make source-check
```

`source-check` проверяет форматирование, запускает `go vet`, тесты с детектором
гонок и тесты на фикстурах. Также проверяет protobuf, контракты и отсутствие
временных файлов для встраивания. Линтер устанавливается и запускается отдельно:

```bash
make lint-install
make lint
```

Полная локальная проверка дополнительно собирает бинарники для других платформ
и контейнерные образы, поэтому требует запущенный Docker daemon:

```bash
make verify
```

### Интеграции, изменяющие локальное состояние

Каждая команда ниже изменяет состояние Docker/kind и отказывается запускаться,
если уже существует `tripgo-local`. Перед запуском убедитесь, что управляемые
кластер и registry не нужны другому процессу:

```bash
make integration-lifecycle       # lab 1: SSA, migrations, persistence и reset
make integration-release-runtime # lab 3: embedded Push image, registry и API
make integration-isolation       # parallel labs 2/4/5 и isolation/recovery
```

Для `integration-isolation` нужны не менее 10 GiB памяти Docker и 15 GiB
свободного места. При очистке проверки учитывают принадлежность ресурсов.
После аварийного завершения перед повторным запуском проверьте `docker ps -a`.

## Релизы

На `push` в `main` GitHub Actions запускает проверки и собирает четыре архива.
После проверок на целевых платформах публикует GitHub Release
`main-<полный commit SHA>`. `latest` указывает на последнюю опубликованную сборку
`main`; отдельного стабильного канала нет. PR проходят те же проверки без
публикации. CI-артефакты хранятся 14 дней.

Локальная команда ничего не публикует. Ей нужны закоммиченные исходники без
изменений в индексе и рабочем каталоге, закреплённый submodule и полный commit
hash. Воспроизводимость проверяется двумя сборками:

```bash
make release-repro-check VERSION=v1.0.0 COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

CI проверяет установку и запуск того же архива на macOS/Linux `amd64`/`arm64`.
На Linux дополнительно выполняет проверку Docker/kind `make release-platform-check`.
Полную Docker-проверку macOS нужно выполнить на машине с Docker перед первым
публичным релизом: hosted macOS arm64 runner не поддерживает вложенную
виртуализацию. Публикуются проверенные архивы без пересборки.
Подробности, порядок первого запуска и очистка описаны в
[документе о релизах](release-v1.md).

## Установка CLI

Установка из публичного релиза:

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL \
  https://github.com/course-go-autumn-2026/course-infra/releases/latest/download/install-tripgoctl \
  | sh
tripgoctl version
tripgoctl doctor
```

Установка не требует Go, Git или GitHub-токена. Нужны `curl`, `tar` и
`sha256sum` либо `shasum`; для работы инфраструктуры нужен запущенный Docker.
Установщик один раз определяет tag `latest`, затем скачивает архив и его
контрольную сумму из одной версии. Он не вызывает `sudo` и не изменяет файлы
настройки shell. По умолчанию выбирается доступный для записи `/usr/local/bin`,
иначе `$HOME/.local/bin`. Если каталога нет в `PATH`, установщик печатает команду
для его добавления.

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
контрольной суммы не заменяет установленный бинарник. SHA-256 проверяет
целостность архива, но не защищает от компрометации репозитория или скрипта.

### Сборка из исходников

`make install` собирает из текущего checkout CLI со встроенными Push Service и
контрактами, затем устанавливает его локально:

```bash
make install
tripgoctl version
```

По умолчанию используется доступный для записи `/usr/local/bin`, иначе
`$HOME/.local/bin`. Целевой каталог и метаданные сборки можно задать явно:

```bash
make install INSTALL_DIR="$HOME/.local/bin" VERSION=dev
```

Команда не использует `sudo` и не изменяет файлы настройки shell. Если выбранного
каталога нет в `PATH`, она печатает точную команду `export PATH=...`.

Локальная сборка CLI для разработки, без встроенных релизных файлов:

```bash
make build-tripgoctl
./build/bin/tripgoctl --help
./build/bin/tripgoctl version
```

## Лицензия

[MIT](../LICENSE). Файл лицензии также включён в release-архивы.
Внешние зависимости сохраняют собственные условия лицензирования.

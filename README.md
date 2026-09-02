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

## Разработка

Требования: Go 1.24+, Docker для container build, Python 3 для contract sync.

```bash
make test-race
make cross-build
make container-build
make lint-install && make lint
make contract-check
# Destructive lifecycle checks; each requires no existing tripgo-local cluster.
make stage5-smoke  # lab 1: PostgreSQL
make stage6-smoke  # lab 2: observability
make stage7-smoke  # standalone Push Service and scratch images
make stage8-smoke  # lab 3: embedded local Push image and Kubernetes lifecycle
make stage9-smoke  # labs 4/5: isolated Redpanda, Console, topics and persistence
make stage10-smoke # parallel labs 2/4/5: mappings, isolation, connect and pod restart

# Local-only deterministic release; never publishes. Use release for one build,
# or release-repro-check for the full two-build reproducibility gate.
make release-repro-check VERSION=v1.0.0 COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

## Установка CLI

Проверенный release устанавливается на macOS/Linux (`amd64`/`arm64`) скриптом,
который выбирает нужный архив и обязательно сверяет его по `SHA256SUMS`:

```bash
./scripts/install-tripgoctl v1.0.0
tripgoctl version
```

По умолчанию используется GitHub Releases. Для локально собранного или
переданного преподавателем набора файлов:

```bash
TRIPGOCTL_RELEASE_DIR="$PWD/build/release" \
  ./scripts/install-tripgoctl --install-dir "$HOME/.local/bin" v1.0.0
```

Скрипт не изменяет shell profiles и не использует `sudo`. Если выбранного
каталога нет в `PATH`, он печатает точную команду `export PATH=...`.

Локальная development-сборка CLI:

```bash
make build-tripgoctl
./build/bin/tripgoctl --help
./build/bin/tripgoctl version
```

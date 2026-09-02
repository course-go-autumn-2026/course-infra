# Design v1: локальное окружение TripGo

Статус: утверждён на gate-ревью этапа 0. Дата: 2026-09-01.

Исходный план: `homework/memory-bank/tripgo-infra-implementation-plan.md`.
Этот документ фиксирует решения этапа 0 для реализации в `tripgo-infra`.

## 1. Scope и границы артефактов

Canonical repository/module:

```text
github.com/course-go-autumn-2026/tripgo-infra
```

Будущий release-артефакт:

```text
tripgoctl
  darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
  + embedded runtime Dockerfile
  + embedded push-service linux/<соответствующий arch>
```

На этапе 1 локальный `make verify` отдельно собирает и тестирует оба бинарника.
На этапе 3 принято новое packaging-решение: remote OCI Push Service не
публикуется. CI надо повторно согласовать до первого общего remote branch/review
процесса; self-contained CLI releases — на этапе 12.

Границы пакетов:

```text
cmd/tripgoctl          -> internal/cli, generator, catalog
cmd/push-service       -> internal/pushservice
internal/shared        -> только узкие технические примитивы без domain logic
```

`internal/cli` и `internal/pushservice` не импортируют друг друга. Kubernetes,
kind lifecycle и студенческий cwd принадлежат CLI. HTTP/gRPC push behaviour
принадлежит Push Service. `templates/` встраиваются в CLI. Канонические API
принадлежат homework.

Студенческий `trip-service` всегда запускается на хосте. Инфраструктура не
собирает его, не применяет его миграции и не требует Dockerfile студента.

## 2. Toolchain и Kubernetes

| Элемент | Решение v1 |
|---|---|
| Go | `1.24.x`; `go 1.24` в модуле, последняя patch в CI |
| kind | `v0.27.0`, helper обязательно проверяется по platform SHA-256 |
| Kubernetes | `kindest/node:v1.32.2@sha256:f226345927d7e348497136874b6d207e0b32cc52154ad8323129352923a3142f` |
| Apply | `client-go`, server-side apply |
| Field manager | `tripgoctl` |
| Cluster | один `tripgo-local` |

Node image и digest взяты из release notes kind v0.27.0. CLI работает только с
кластером, содержащим собственный immutable identity marker. Kube-context сам по
себе не считается достаточным доказательством ownership.

SSA выполняется с field manager `tripgoctl`; force conflicts по умолчанию
запрещён. Конфликт с полями другого manager возвращает exit code `4` и
диагностику. Это предотвращает молчаливое затирание ручных изменений.

## 3. Публичный configuration contract

`./environment.toml` декодируется строго: неизвестные поля, неизвестные и
повторяющиеся компоненты запрещены. Состав компонентов сравнивается как set:
порядок в пользовательском TOML не влияет на валидность или генерацию.
Canonical fixtures используют порядок таблицы ниже, а генератор всегда
нормализует вход во внутренний стабильный порядок.

```toml
schema_version = 1
lab = 1
components = ["postgres"]

# Необязательные пользовательские переменные для generated .env:
[env]
FEATURE_FLAG = "enabled"
```

`env` — необязательная таблица строк. Имена соответствуют
`[A-Za-z_][A-Za-z0-9_]*`, значения являются literal single-line strings без
interpolation, NUL и переводов строк. Имена всех встроенных переменных
зарезервированы сразу для labs 1–5 и не могут быть переопределены. Custom env
добавляются после встроенных в лексикографическом порядке. Пустая или
отсутствующая таблица сохраняет минимальный исходный контракт.

| Lab | Components |
|---:|---|
| 1 | `postgres` |
| 2 | `postgres, observability` |
| 3 | `postgres, observability, push` |
| 4 | `postgres, observability, push, redpanda` |
| 5 | `postgres, observability, push, redpanda` |

Формальная модель: [`../schemas/environment-v1.schema.json`](../schemas/environment-v1.schema.json).
Парсер сначала проверяет TOML syntax/types, затем эквивалентные schema и
cross-field constraints. Ошибка содержит путь, поле и ожидаемое значение.

## 4. Ports

Для lab `N`:

```text
host base     = 20000 + N * 1000
nodePort base = 30000 + N * 100
```

Все mappings слушают только `127.0.0.1`, protocol TCP. `cluster start`
резервирует все 50 lab mappings, включая компоненты, не используемые конкретной
работой, и отдельно managed registry port `127.0.0.1:5001`.

| # | Component endpoint | Host offset | NodePort |
|---:|---|---:|---:|
| 1 | Redpanda Console | 81 | `nodePortBase + 1` |
| 2 | Redpanda Kafka | 92 | `nodePortBase + 2` |
| 3 | PostgreSQL | 32 | `nodePortBase + 3` |
| 4 | Grafana | 300 | `nodePortBase + 4` |
| 5 | OTel gRPC | 317 | `nodePortBase + 5` |
| 6 | OTel HTTP | 318 | `nodePortBase + 6` |
| 7 | Jaeger | 686 | `nodePortBase + 7` |
| 8 | Push HTTP/admin | 809 | `nodePortBase + 8` |
| 9 | Push gRPC | 905 | `nodePortBase + 9` |
| 10 | Prometheus | 909 | `nodePortBase + 10` |

Пример lab 4: host `24081, 24092, 24032, 24300, 24317, 24318, 24686,
24809, 24905, 24909`; NodePort `30401..30410`.

Проверка формул:

- host blocks `21000..21999` — `25000..25999` не пересекаются;
- реально используемые offsets уникальны и входят в block;
- NodePort `30101..30510` входит в стандартный диапазон `30000..32767`;
- port студенческого HTTP `8080` и gRPC `9091` не управляются CLI.

## 5. Namespace и ownership

Namespace: `tripgo-lab-%02d`.

Обязательные labels каждого namespaced resource:

```yaml
app.kubernetes.io/managed-by: tripgoctl
app.kubernetes.io/part-of: tripgo-local
tripgo.course/lab: "01"
tripgo.course/environment: tripgo-lab-01
```

Аннотации:

```yaml
tripgo.course/generator-version: v1.0.0
tripgo.course/cluster-id: <generated-cluster-identity>
```

Selector labels компонентов отделены от ownership labels и неизменны в v1.
Удаление namespace/reset разрешено только при совпадении имени, всех ownership
labels и cluster identity annotation. CLI не удаляет отдельные произвольные
ресурсы без ownership.

## 6. Lifecycle

Подробный UX: [`cli-contract-v1.md`](cli-contract-v1.md).

- `cluster stop`: показывает environments, требует `yes`/`--yes`, удаляет весь
  kind cluster и все данные;
- `environment stop`: scale-to-zero, сохраняет namespace и PVC;
- `environment start`: reconcile + scale-up + readiness + atomic `.env`;
- `environment reset`: требует `yes`/`--yes`, удаляет namespace и PVC;
- `connect`: read-only, печатает постоянные endpoints и завершается.

Несколько labs могут быть ready одновременно. Один lab соответствует ровно
одному namespace.

## 7. `.env` и локальное состояние

`.env` заменяется только при наличии точного tripgoctl marker в первой строке.
Запись идёт через temp file в том же каталоге, `fsync`, atomic rename. При
пользовательском `.env` операция завершается до применения Kubernetes-изменений,
чтобы исключить готовую инфраструктуру без пригодного config-файла.

`.tripgo/rendered` — диагностическая проекция последнего desired state и
атомарно обновляется при каждом `environment start`; локальный lock не хранится.
Фактические ownership, cluster identity, image digests и readiness проверяются
по Kubernetes API. Неизвестные соседние файлы в `.tripgo` сохраняются.

Глобальный state хранится в OS user cache/config directory и содержит kind
version, cluster identity, config hash и известные namespace. Kubernetes
ownership остаётся источником истины при восстановлении повреждённого state.

## 8. Синхронизация Push contracts

Канонический источник подключён как git submodule:

```text
third_party/homework -> git@github.com:course-go-autumn-2026/homework.git
```

Используемые файлы:

```text
contracts/openapi/push-service.openapi.yaml
contracts/proto/push/v1/push.proto
```

`api/openapi/push-service.openapi.yaml` и `api/proto/push/v1/push.proto` —
relative symlink непосредственно на эти canonical файлы в submodule; вторые
копии contract content не хранятся. `api/source.json` фиксирует repository,
точный submodule commit, superproject gitlink, relative link targets и SHA-256
canonical content. Contract gate также требует, чтобы каждый canonical byte
stream точно присутствовал в pinned commit, и отклоняет dirty contract paths.
`scripts/sync-contracts sync` атомарно создаёт или исправляет links, а `check`
проверяет тип link, точный target, существование canonical файла и metadata без
fallback copy. Поэтому initialized submodule обязателен для build/check; Windows
не поддерживается v1, а заявленные Darwin/Linux platforms поддерживают symlink.
Поскольку Go `go:embed` не следует symlink и запрещает `..`, release build после
contract check атомарно staging-ит проверенные bytes в ignored package-local
`internal/contractasset/generated/`. Tagged `tripgoctl` напрямую импортирует
provider и встраивает OpenAPI, proto и source manifest; cross-build доказывает
exact content каждого из четырёх CLI, runtime provider проверяет manifest SHA, а
staging очищается trap-ом. Push binary не дублирует source contracts без runtime
причины: self-contained публикуемым артефактом является CLI, а Push runtime уже
содержит compiled handlers/descriptors. Workflow `.github/workflows/release-platform-check.yml` существует, но ещё не
запускался и сам по себе не доказывает native execution. Его checkout обязан
использовать initialized pinned submodule и не обращаться к плавающей ветке;
native Linux и чистая внешняя машина остаются непроверенными release gates.

После canonical contract change обновляются submodule commit, symlink metadata
и generated bindings одним commit `tripgo-infra`.

Канонический Push proto публикует `go_package`
`github.com/course-go-autumn-2026/homework/gen/push/v1`; инфраструктурные
bindings по-прежнему генерируются во внутренний package через явный mapping в
`buf.gen.yaml`.

## 9. Push image, local registry и releases

Управляемый registry:

```text
host:       127.0.0.1:5001
kind alias: localhost:5001
repository: localhost:5001/tripgo-push-service
```

`cluster start` создаёт registry container с ownership marker и настраивает kind
containerd mirror. Каждый release `tripgoctl` содержит runtime Dockerfile и
готовый `push-service` для `linux/<host arch>`. CLI извлекает private temporary
build context, выполняет локальный scratch build, пушит image только в managed
registry, получает digest и передаёт immutable reference генератору. Mutable tag
не попадает в Kubernetes manifest/lock.

`cluster stop` удаляет и kind cluster, и принадлежащий CLI registry. Порт 5001
проверяется вместе с остальными host ports. Remote OCI Push Service не
публикуется; перед Kubernetes-интеграцией обязательны contract tests embedded
binary и local build/push/run integration checks на amd64/arm64.

Этап 12 реализует двухфазный embedding и локальные archives с `SHA256SUMS`.
GitHub Release и любая иная публикация не выполняются без отдельного решения.

## 10. Бумажная проверка labs 1–5

| Lab | `environment start` | Host workflow | Dockerfile студента? |
|---:|---|---|:---:|
| 1 | namespace + PostgreSQL + `.env` | `make migrate`, `make run`, HTTP `:8080` | нет |
| 2 | lab 1 + isolated OTel/Prometheus/Grafana/Jaeger | service exports OTLP to `localhost:22318` | нет |
| 3 | lab 2 + Push | service calls HTTP `localhost:23809` | нет |
| 4 | lab 3 + Redpanda/topics | Push gRPC `24905`, Kafka `24092`, own gRPC `:9091` | нет |
| 5 | isolated full stack + resilience env | same components on lab 5 ports | нет |

Для каждой работы CLI генерирует полный накопительный env-набор. Миграции
остаются в студенческом repository и запускаются с хоста. Endpoints доступны
после завершения CLI благодаря kind extraPortMappings + NodePort.

## 11. Интеграция с homework

Расхождения, зафиксированные на этапе 0, закрыты этапами 7 и 13:

- student docs и task README используют `tripgoctl`, lab-specific ports и
  generated `.env` вместо `tripgo-devenv`/`make up`;
- canonical OpenAPI содержит `400` для невалидного `/admin/behaviour`;
- canonical Push proto содержит course-owned `go_package`;
- `api/` сохраняет только проверяемые relative symlink на pinned homework, а
  release staging остаётся ignored и очищается после embedding.

## 12. Acceptance criteria v1

Приняты 15 общих критериев раздела 10 исходного плана. Дополнительно этап 0
считается завершённым, когда пользователь утверждает:

1. repository/module/image naming и временный build-only release режим;
2. CLI commands, human-only output, exit codes и destructive confirmations;
3. TOML schema и set-equivalence для components;
4. port formulas/order и loopback binding;
5. ownership labels/annotations и SSA conflict policy;
6. submodule contract sync и необходимость upstream OpenAPI change;
7. lifecycle и `.env` marker policy;
8. список расхождений с homework.

## 13. Решения итерации gate-ревью

Пользователь подтвердил:

1. `environment reset` требует интерактивное `yes`, автоматизация использует
   `--yes`;
2. все host ports публикуются только на `127.0.0.1`;
3. `components` валидируются как set, порядок не является частью контракта;
4. `environment logs <component> --follow` входит в v1.

Открытых архитектурных вопросов этапа 0 нет. Gate этапа 0 утверждён
пользователем 2026-09-01; решения перенесены в журнал исходного плана.

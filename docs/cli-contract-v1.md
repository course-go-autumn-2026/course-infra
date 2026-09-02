# Публичный контракт `tripgoctl` v1

Статус: утверждён на gate-ревью этапа 0 (2026-09-01).

## Общие правила

- CLI поддерживает macOS и Linux на `amd64` и `arm64`.
- Пользовательский вывод только человекочитаемый. JSON-режима в v1 нет.
- Информационные сообщения идут в stdout, предупреждения и ошибки — в stderr.
- Ошибка содержит причину и, когда применимо, следующую команду пользователя.
- Цвет используется только для TTY; смысл не должен зависеть от цвета.
- Интерактивные вопросы задаются только при TTY. В non-TTY destructive-команда
  завершается ошибкой, если не передан `--yes`.
- Все environment-команды, кроме `environment list`, читают
  `./environment.toml`. Переопределения пути в v1 нет.
- CLI не использует глобальный текущий каталог: cwd передаётся в application
  layer как зависимость.

## Команды

```text
tripgoctl version
tripgoctl help

tripgoctl cluster start
tripgoctl cluster status
tripgoctl cluster stop [--yes]

tripgoctl environment start
tripgoctl environment status
tripgoctl environment stop
tripgoctl environment reset [--yes]
tripgoctl environment logs <component> [--follow] [--tail <lines>]
tripgoctl environment list

tripgoctl connect
```

Отдельных `environment validate`, `open` и JSON-команд в v1 нет.

## Коды завершения

| Код | Значение |
|---:|---|
| `0` | команда успешно завершена |
| `1` | операция завершилась ошибкой |
| `2` | ошибка аргументов или `environment.toml` |
| `3` | не выполнено предусловие: Docker/cluster/environment/readiness |
| `4` | безопасный конфликт: порт, чужой cluster identity, пользовательский `.env`, SSA conflict |
| `130` | операция отменена сигналом или пользователем |

## `version` и help

```text
$ tripgoctl version
tripgoctl v1.0.0 (commit abcdef1, built 2026-09-01T00:00:00Z)
```

Dev-сборка печатает версию `dev`, commit `unknown` допустим только вне CI.
Корневая справка перечисляет группы `cluster`, `environment`, команду `connect`
и примеры первого запуска.

## Cluster lifecycle

### `cluster start`

1. Проверяет платформу, Docker CLI и daemon.
2. Проверяет 50 зарезервированных lab host ports и managed registry port
   `127.0.0.1:5001`, печатает полный список конфликтов за один запуск.
3. Проверяет ресурсы хоста и предупреждает о недостатке памяти/диска.
4. Получает закреплённый `kind` из кеша либо скачивает и проверяет SHA-256.
5. Идемпотентно создаёт кластер `tripgo-local` и identity marker.

Успешный итог:

```text
Cluster: tripgo-local
Status:  ready
Kind:    v0.27.0
Kubernetes: v1.32.2

Next: cd <lab-directory> && tripgoctl environment start
```

Повторный start возвращает `0`, если identity и port mappings совпадают. При
несовпадении конфигурации CLI не пересоздаёт кластер автоматически.

### `cluster status`

Показывает имя, identity, версии, readiness control plane, hash port mappings и
количество известных/запущенных лабораторных окружений. Отсутствующий кластер —
код `3`.

### `cluster stop [--yes]`

Показывает все обнаруженные `tripgo-lab-XX`, предупреждает об удалении всех
PVC и требует точного ответа `yes`. `--yes` отключает вопрос. Удаляется только
кластер с корректным identity marker; отсутствие кластера считается
идемпотентным успехом.

## Environment lifecycle

Namespace имеет вид `tripgo-lab-01` … `tripgo-lab-05`.

### `environment start`

Валидирует TOML до мутаций, проверяет cluster identity, применяет требуемое
состояние через Kubernetes server-side apply, создаёт топики, ждёт readiness,
атомарно обновляет `.tripgo/rendered` и `.env`. Локальный lock не сохраняется;
повторный start всегда перегенерирует manifests.

Итог содержит lab, namespace, состояния компонентов и команду `tripgoctl
connect`. Повторный start reconciles окружение и возвращает `0`.

### `environment status`

Показывает namespace и по каждому ожидаемому компоненту desired/ready replicas,
readiness и сохранённый digest образа. Команда ничего не изменяет.

### `environment stop`

Масштабирует управляемые workloads до нуля. Namespace, PVC и данные
сохраняются. Повторный stop возвращает `0`.

### `environment reset [--yes]`

Показывает namespace и предупреждает об удалении данных. Требует `yes` или
`--yes`. Удаляет только namespace с полным набором ownership labels. Локальный
`.env` удаляется только по marker; из `.tripgo` удаляется только `rendered`, а
неизвестные соседние файлы сохраняются. Отсутствующий namespace — идемпотентный
успех.

### `environment list`

Не требует `environment.toml`. Показывает все принадлежащие tripgoctl namespace:

```text
LAB  NAMESPACE       STATE    COMPONENTS
01   tripgo-lab-01   ready    postgres
03   tripgo-lab-03   stopped  postgres,otel-collector,jaeger,prometheus,grafana,push-service
```

### `environment logs <component> [--follow] [--tail N]`

Для labs 3–5 поддерживается `push-service`; labs 4–5 также поддерживают
`redpanda`, `redpanda-console` и `redpanda-topic-reconciler`. Pod выбирается по
точным ownership labels текущего namespace, а не по подстроке. Долгий `--follow`
не удерживает lifecycle mutation lease после проверки ownership.

## `connect`

Только читает состояние, проверяет existence/readiness, печатает фиксированные
host endpoints и необходимые credentials и завершается. Для Grafana в labs 2–5
печатаются `admin` / `admin`; anonymous Admin-доступ также включён. Команда не
запускает port-forward, не меняет `.env` и lock. Повторный вызов детерминирован
при неизменном состоянии.

Пример для работы 1:

```text
Environment: tripgo-lab-01 (ready)

COMPONENT   ADDRESS
PostgreSQL  localhost:21032

DATABASE_URL=postgres://tripgo:tripgo@localhost:21032/tripgo?sslmode=disable
psql "postgres://tripgo:tripgo@localhost:21032/tripgo?sslmode=disable"
```

## Правила `.env`

Маркер первой строки строго равен:

```text
# Generated by tripgoctl. Do not edit manually.
```

- файла нет — создать атомарно;
- первая строка равна маркеру — атомарно заменить;
- любой другой существующий файл — код `4`, без частичной записи;
- `connect` файл не меняет;
- `reset` удаляет файл только при совпадении маркера;
- generated-файл имеет mode `0600` на Unix.

## Совместимость

- добавление новых необязательных строк human-readable вывода допустимо в minor;
- имена команд, флагов, exit codes, lifecycle и marker `.env` стабильны в v1;
- schema/state несовместимость должна завершаться до мутаций и предлагать
  совместимую версию CLI или reset.

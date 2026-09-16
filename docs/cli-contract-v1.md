# Публичный контракт `tripgoctl` v1

Статус: утверждён на gate-ревью этапа 0 (2026-09-01).

## Общие правила

- CLI поддерживает macOS и Linux на `amd64` и `arm64`.
- Пользовательский вывод только человекочитаемый. JSON-режима в v1 нет.
- Информационные сообщения идут в stdout, предупреждения и ошибки — в stderr.
- Ошибка содержит причину и, когда применимо, команду для следующего действия.
- Цвет используется только для TTY; смысл не должен зависеть от цвета.
- Интерактивные вопросы задаются только при TTY. Без TTY команда, удаляющая
  ресурсы или данные, завершается ошибкой, если не передан `--yes`.
- Все environment-команды, кроме `environment list`, читают
  `./environment.toml`. Переопределения пути в v1 нет.
- CLI не использует глобальный текущий каталог: cwd передаётся в прикладной
  слой как зависимость.

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

Сборка для разработки печатает версию `dev`; commit `unknown` допустим только вне CI.
Корневая справка перечисляет группы `cluster`, `environment`, команду `connect`
и примеры первого запуска.

## Cluster lifecycle

### `cluster start`

1. Проверяет платформу, Docker CLI и daemon.
2. Проверяет 50 зарезервированных портов работ на хосте и порт управляемого
   registry `127.0.0.1:5001`, печатает полный список конфликтов за один запуск.
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

Повторный start возвращает `0`, если совпадают identity и пробросы портов.
При несовпадении конфигурации CLI не пересоздаёт кластер автоматически.

### `cluster status`

Показывает имя, identity, версии, готовность control plane, hash пробросов портов
и количество известных и запущенных лабораторных окружений. Если кластера нет,
возвращается код `3`.

### `cluster stop [--yes]`

Показывает все обнаруженные `tripgo-lab-XX`, предупреждает об удалении всех
PVC и требует точного ответа `yes`. `--yes` отключает вопрос. Удаляется только
кластер с корректным identity marker; отсутствие кластера считается
идемпотентным успехом.

## Environment lifecycle

Namespace имеет вид `tripgo-lab-01` … `tripgo-lab-05`.

### `environment start`

Проверяет TOML до любых изменений, сверяет identity кластера, применяет
требуемое состояние через Kubernetes server-side apply, создаёт топики и ждёт
готовности. Атомарно обновляет `.tripgo/rendered` и `.env`. Локальный lock не
сохраняется; повторный start всегда генерирует манифесты заново.

Вывод содержит работу, namespace, состояния компонентов и команду
`tripgoctl connect`. Повторный start приводит окружение к требуемому состоянию
и возвращает `0`.

### `environment status`

Показывает namespace, а для каждого ожидаемого компонента число требуемых и
готовых реплик, готовность и сохранённый digest образа. Команда ничего не изменяет.

### `environment stop`

Уменьшает число реплик управляемых workloads до нуля. Namespace, PVC и данные
сохраняются. Повторный stop возвращает `0`.

### `environment reset [--yes]`

Показывает namespace и предупреждает об удалении данных. Требует `yes` или
`--yes`. Удаляет только namespace с полным набором меток владения. Локальный
`.env` удаляется только по маркеру; из `.tripgo` удаляется только `rendered`, а
неизвестные соседние файлы сохраняются. Отсутствие namespace считается
идемпотентным успехом.

### `environment list`

Не требует `environment.toml`. Показывает все принадлежащие tripgoctl namespace:

```text
LAB  NAMESPACE       STATE    COMPONENTS
01   tripgo-lab-01   ready    postgres
03   tripgo-lab-03   stopped  postgres,otel-collector,jaeger,prometheus,grafana,push-service
```

### `environment logs <component> [--follow] [--tail N]`

Для работ 3–5 поддерживается `push-service`; для работ 4–5 также доступны
`redpanda`, `redpanda-console` и `redpanda-topic-reconciler`. Pod выбирается по
точным меткам владения текущего namespace, а не по подстроке. Долгий `--follow`
не удерживает блокировку изменений жизненного цикла после проверки владения.

## `connect`

Только читает состояние, проверяет наличие и готовность ресурсов, печатает постоянные
адреса на хосте и необходимые учётные данные, затем завершается. Для Grafana
в работах 2–5 печатаются `admin` / `admin`; анонимный Admin-доступ также включён.
Команда не запускает port-forward, не меняет `.env` и lock. При неизменном
состоянии повторный вызов возвращает тот же результат.

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
- сгенерированный файл имеет права `0600` на Unix.

## Совместимость

- в minor-версиях допустимы новые необязательные строки человекочитаемого вывода;
- имена команд, флагов, коды завершения, жизненный цикл и маркер `.env` стабильны в v1;
- при несовместимости схемы или состояния команда должна завершиться до изменений
  и предложить совместимую версию CLI или reset.

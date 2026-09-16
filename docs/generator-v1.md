# Детерминированный generator v1

Этап 3, 2026-09-01.

## API и входы

`internal/generator.Generate` принимает только:

1. уже проверенный и нормализованный `environment.Config`;
2. доверенные настройки приложения: версию `tripgoctl`, identity кластера и
   временную ссылку на образ Push.

Время, случайность, cwd и порядок TOML maps не входят в параметры генератора.
Пользовательская `[env]` не попадает в манифесты: её будет использовать только
модуль записи `.env` на этапе 5.

Образ Push Service собирается локально из встроенных Dockerfile и Linux-бинарника
и никогда не публикуется во внешнем OCI registry. Пока образ не появился в
управляемом registry, генерация для работ 3–5 возвращает ошибку. Golden-тесты передают через
внутренний `Options` синтаксически корректную ссылку
`localhost:5001/tripgo-push-service@sha256:<64 hex>`. Передать её через
`environment.toml` нельзя; ссылки на другой репозиторий и изменяемые tag запрещены.

## Выход

```text
.tripgo/
  rendered/
    namespace.yaml
    postgres.yaml
    observability.yaml  # lab >= 2
    push.yaml           # lab >= 3
    redpanda.yaml       # lab >= 4
    topics.yaml         # lab >= 4
```

Порядок файлов всегда соответствует списку выше. `Write` создаёт закрытые
каталоги с правами `0700` и файлы с правами `0600`, атомарно заменяет
`.tripgo/rendered`, удаляет устаревшие сгенерированные манифесты и сохраняет
неизвестные соседние файлы в `.tripgo`. Локальный lock не создаётся: каждый start
заново строит манифесты требуемого состояния. Принадлежность ресурсов и их
фактическое состояние проверяются по Kubernetes API.

## Kubernetes resources

| Файл | Ресурсы |
|---|---|
| `namespace.yaml` | Namespace |
| `postgres.yaml` | Secret с локальными course credentials, PVC 1 GiB, Deployment, NodePort Service |
| `observability.yaml` | configs, OTel Collector, Jaeger, Prometheus и Grafana; два PVC; host NodePorts для OTLP/UI |
| `push.yaml` | Push Deployment и двухпортовый NodePort Service |
| `redpanda.yaml` | internal/external Services, single-node StatefulSet с PVC, Console Deployment/NodePort Service |
| `topics.yaml` | desired-state ConfigMap с partition contract `3/3/1` |

ConfigMap с топиками монтируется в постоянный управляемый Deployment-reconciler.
Он идемпотентно создаёт отсутствующие топики, увеличивает недостающее число
партиций до `3/3/1` и отказывается уменьшать его с потерей данных. Проверка
состояния сверяет точные данные и скрипт управляемого ConfigMap.

Каждый ресурс верхнего уровня содержит полный набор меток владения,
generator-version и cluster-id. Шаблоны pod содержат те же метки владения;
метки селекторов отделены. Образы указаны только по digest манифеста.
У workloads заданы запросы ресурсов, ограничения памяти, пробы и ограниченные
security contexts.

## Golden contract

Эталонные каталоги: `tests/golden/lab-{1..5}/.tripgo`. Число ресурсов:

| Lab | Files | Kubernetes resources |
|---:|---:|---:|
| 1 | 2 | 5 |
| 2 | 3 | 19 |
| 3 | 4 | 21 |
| 4 | 6 | 27 |
| 5 | 6 | 27 |

`internal/generator/generator_test.go` проверяет:

- побайтовое совпадение результата с эталонами всех работ;
- совпадение результатов двух генераций;
- синтаксис каждого YAML с несколькими документами;
- аннотации владения и namespace;
- отсутствие пользовательских env в манифестах;
- отказ от изменяемого, отсутствующего или нелокального образа Push;
- повторяемость записи, очистку устаревших файлов, запрет символических ссылок
  в выводе и сохранение неизвестных файлов в `.tripgo`.

Команда для явного обновления эталонов:

```bash
UPDATE_GOLDEN=1 go test ./internal/generator
```

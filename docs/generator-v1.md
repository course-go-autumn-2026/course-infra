# Детерминированный generator v1

Статус: реализация этапа 3, 2026-09-01.

## API и входы

`internal/generator.Generate` принимает только:

1. уже проверенный и нормализованный `environment.Config`;
2. trusted application options: версия `tripgoctl`, cluster identity и временный
   Push image reference.

Время, случайность, cwd и порядок TOML maps не являются входами. Custom `[env]`
не входит в manifests и не может утечь туда; она будет использована только
writer-ом `.env` на этапе 5.

Push Service строится локально из embedded Dockerfile + Linux binary и никогда
не публикуется как remote OCI. Поэтому генерация labs 3–5 возвращает ошибку до
появления image в managed registry. Golden tests передают через внутренний
`Options` syntactically valid
`localhost:5001/tripgo-push-service@sha256:<64 hex>`. Input недоступен из
`environment.toml`; ссылки на другой repository и mutable tags запрещены.

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

Порядок bundle всегда соответствует списку выше. `Write` создаёт private
каталоги `0700` и файлы `0600`, атомарно заменяет `.tripgo/rendered`, удаляет
stale generated manifests и сохраняет неизвестные соседние файлы в `.tripgo`.
Локальный lock не создаётся: каждый start всегда строит desired manifests
заново, а ownership и фактическое состояние проверяются по Kubernetes API.

## Kubernetes resources

| Файл | Ресурсы |
|---|---|
| `namespace.yaml` | Namespace |
| `postgres.yaml` | Secret с локальными course credentials, PVC 1 GiB, Deployment, NodePort Service |
| `observability.yaml` | configs, OTel Collector, Jaeger, Prometheus и Grafana; два PVC; host NodePorts для OTLP/UI |
| `push.yaml` | Push Deployment и двухпортовый NodePort Service |
| `redpanda.yaml` | internal/external Services, single-node StatefulSet с PVC, Console Deployment/NodePort Service |
| `topics.yaml` | desired-state ConfigMap с partition contract `3/3/1` |

Topics ConfigMap монтируется в постоянный owned reconciler Deployment. Он
идемпотентно создаёт отсутствующие topics, увеличивает недостающее число
partitions до `3/3/1` и отказывается от разрушающего уменьшения partitions.
Lifecycle status проверяет exact managed ConfigMap data и script.

Каждый top-level resource имеет полный ownership labels, generator-version и
cluster-id. Pod templates имеют тот же ownership; selector labels отделены.
Images указаны только по manifest digest. Workloads имеют resource requests,
memory limits, probes и restricted security contexts.

## Golden contract

Golden directories: `tests/golden/lab-{1..5}/.tripgo`. Число ресурсов:

| Lab | Files | Kubernetes resources |
|---:|---:|---:|
| 1 | 2 | 5 |
| 2 | 3 | 19 |
| 3 | 4 | 21 |
| 4 | 6 | 27 |
| 5 | 6 | 27 |

`internal/generator/generator_test.go` проверяет:

- byte-for-byte golden output всех labs;
- две генерации без diff;
- синтаксис каждого multi-document YAML;
- ownership/namespace annotations;
- отсутствие custom env в manifests;
- отказ от mutable/отсутствующего либо не-local Push image;
- repeatable writer, stale cleanup, защиту от symlinked output и сохранение
  неизвестных файлов в `.tripgo`.

Обновление golden является явной операцией:

```bash
UPDATE_GOLDEN=1 go test ./internal/generator
```

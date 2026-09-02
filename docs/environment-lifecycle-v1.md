# Environment lifecycle v1: labs 1–5

Реализованы полные lifecycle-сценарии работ 1–5: PostgreSQL, observability,
локально построенный Push Service и изолированные Redpanda/Console в работах 4–5.

## Reconciliation

`tripgoctl environment start`:

1. разбирает `<cwd>/environment.toml` и принимает labs 1–5;
2. до Kubernetes mutations показывает число уже запущенных окружений и bounded
   Docker-memory warning для тяжёлых labs;
3. до Kubernetes mutations отклоняет пользовательский `.env` без marker;
4. получает kubeconfig и API identity только от verified kind-кластера под
   общим operation lease;
5. проверяет полное ownership metadata каждого существующего ресурса;
6. для labs 3–5 извлекает embedded Dockerfile и Linux Push binary архитектуры CLI,
   строит scratch image, пушит только в managed `localhost:5001` и получает
   immutable RepoDigest;
7. передаёт Push digest генератору через trusted internal options и применяет
   Namespace, конфигурацию, хранилища, Deployments и Services server-side apply
   manager-ом `tripgoctl`, без force conflicts;
8. ждёт observed generation и updated/ready/available replicas всех workloads;
9. синхронизирует known/running state и пишет `.tripgo/`; `.env` устанавливается
   atomic rename после fsync bytes и затем fsync родительского каталога.

Lab 1 публикует PostgreSQL. Lab 2 дополнительно публикует OTel HTTP/gRPC,
Grafana, Jaeger и Prometheus и проверяет namespace-local telemetry Services.
Lab 3 дополнительно публикует Push HTTP/admin и gRPC на фиксированных host ports
`23809` и `23905`; Deployment закреплён по
`localhost:5001/tripgo-push-service@sha256:...`. Labs 4–5 добавляют собственные
single-node Redpanda StatefulSet с PVC, Console и фиксированные Kafka/Console
ports. Постоянный Deployment-reconciler читает desired topics ConfigMap,
создаёт отсутствующие топики, безопасно увеличивает число партиций и отказывается
от разрушающего уменьшения; точные desired counts — `3/3/1`.

## Lifecycle semantics

- `environment status` проверяет ownership, точные catalog Services, immutable
  image digests и generation-aware readiness всех компонентов текущей работы;
- `environment stop` после ownership preflight масштабирует все Deployments и
  Redpanda StatefulSet до нуля полным SSA desired state, сохраняя PVC;
- повторный `environment start` восстанавливает workloads и данные; неизменный
  Push digest не перезапускает pod и сохраняет его in-memory admin settings;
- после фактического рестарта Push pod настройки предсказуемо возвращаются к env
  defaults;
- `environment reset` требует точное `yes` или `--yes`, удаляет только owned
  namespace и распознанные generated `.env`/`.tripgo`;
- `environment list` обнаруживает owned namespaces по labels и cluster identity,
  показывает полный workload-состав и сохраняет повреждённые owned environments
  в списке со state `degraded`;
- `environment logs` поддерживает Push Service, Redpanda, Console и topic
  reconciler, выбирает owned pod в точном namespace; долгий follow не удерживает
  mutation lease;
- `connect` проверяет readiness, печатает постоянные endpoints текущей работы и
  завершается без изменения state или `.env`.

## Safety boundaries

Lifecycle не использует текущий kube context, не принимает image override из
`environment.toml`, не публикует Push Service во внешний registry, не форсирует
SSA conflicts и не удаляет чужие ресурсы. Generated-directory ownership требует
regular non-symlink lock с совпадающими lab, namespace и cluster identity.
Missing prerequisites используют exit code `3`, ownership/SSA/user-file
conflicts — exit code `4`.

## Reproducible checks

Destructive integration gates требуют отсутствующего `tripgo-local` перед запуском:

- `make integration-lifecycle` — lab 1 PostgreSQL lifecycle, migrations, user
  file protection и SSA conflict;
- `make integration-release-runtime` — release-like embedded build/push/pull by
  digest, Push HTTP/gRPC/admin/logs, pod restart и lab 3 stop/start/reset;
- `make integration-isolation` — bounded simultaneous labs 2/4/5, exact
  host↔kind↔NodePort↔env mappings, broken-pipeline recovery, independent
  PostgreSQL/Push/Redpanda/groups, exact topic reconciliation and reset,
  positive/negative telemetry markers, stable read-only `connect` and endpoint
  recovery after pod replacement.

# Встроенный каталог v1

Статус: реализация этапа 2, 2026-09-01.

Каталог компилируется в `tripgoctl` из `internal/catalog`; пользователь не может
переопределить образы, порты или defaults через `environment.toml`.

## OCI images

Runtime использует только форму `repository@sha256:...`. Tag хранится как
provenance и не участвует в pull.

| Компонент | Tag при проверке | Multi-arch manifest digest |
|---|---|---|
| PostgreSQL | `docker.io/library/postgres:16.10-alpine` | `sha256:029660641a0cfc575b14f336ba448fb8a75fd595d42e1fa316b9fb4378742297` |
| OTel Collector contrib | `docker.io/otel/opentelemetry-collector-contrib:0.159.0` | `sha256:1f2c54a30e713fac6b3ae77a1ec84010c2007e29ced8ec666214fc2f6739c1cc` |
| Prometheus | `docker.io/prom/prometheus:v3.14.0` | `sha256:5ce7540c3c00ef4ab0c9d2c995c6a5b9c421f44b4a115d97a2c7af3b1c21cbb0` |
| Grafana | `docker.io/grafana/grafana:13.2.0` | `sha256:3fd54ae1214669f8355f065ec9f6445d5279a3d77095ab048ca045685272429b` |
| Jaeger | `docker.io/jaegertracing/jaeger:2.20.0` | `sha256:46a886260e04002d8f45e213fc39063fa11a50446048fdaa64786fc0840cb9f8` |
| Redpanda | `docker.io/redpandadata/redpanda:v26.2.2` | `sha256:468bd13a9f2bd24794cb7fddc867c767fb1008b9a07b297b89fde48c564d7d96` |
| Redpanda Console | `docker.redpanda.com/redpandadata/console:v3.11.0` | `sha256:bf5ed3ee83e5b2dd6d1c1f1c8095ba252f384ce9354e7a3d2f780f0b4a486c09` |
| Local Registry | `docker.io/library/registry:2.8.3` | `sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373` |

Каждый manifest проверен командой `docker buildx imagetools inspect <image>` и
содержит `linux/amd64` и `linux/arm64`. Digest — digest manifest list/index, а не
одной платформы.

Push Service использует локальный repository
`localhost:5001/tripgo-push-service`. Catalog намеренно не содержит его tag или
digest: `tripgoctl` строит embedded binary через embedded runtime Dockerfile,
пушит только в managed registry и передаёт полученный digest генератору. Remote
OCI publication не выполняется.

## Ports

`internal/catalog/ports.go` содержит десять endpoint в фиксированном порядке.
Для lab `N`:

```text
hostPort = 20000 + N*1000 + hostOffset
nodePort = 30000 + N*100 + nodePortOffset
listen    = 127.0.0.1/TCP
```

`cluster start` будет резервировать все десять mappings для каждой из пяти
работ, независимо от фактического состава lab. Managed registry отдельно
занимает `127.0.0.1:5001` (внутри container — `5000`). Unit-тест проверяет 50
уникальных lab host ports, отсутствие коллизии registry, 50 уникальных NodePort
и стандартный NodePort range.

## Environment defaults

`EnvironmentForLab` возвращает namespace, нормализованный список components,
ports и упорядоченный полный набор переменных `.env`. Наборы накопительные:

| Lab | Components | Число env-переменных |
|---:|---|---:|
| 1 | postgres | 9 |
| 2 | + observability | 14 |
| 3 | + push | 20 |
| 4 | + redpanda | 27 |
| 5 | тот же stack + resilience/choice defaults | 42 |

Все функции каталога возвращают копии slices, поэтому вызывающий код не может
изменить встроенное состояние.

Опциональная таблица `[env]` из `environment.toml` не является частью defaults:
parser проверяет имена и literal single-line значения, запрещает коллизии со
всеми 42 встроенными именами, сортирует custom env по имени и только затем
добавляет их после встроенного набора соответствующей работы.

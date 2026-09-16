# Встроенный каталог v1

Этап 2, 2026-09-01.

Каталог из `internal/catalog` встраивается в `tripgoctl` при компиляции.
Пользователь не может переопределить образы, порты или значения по умолчанию
через `environment.toml`.

## OCI images

Во время работы образы скачиваются только по ссылкам вида `repository@sha256:...`.
Tag хранится для учёта происхождения образа; при скачивании он не используется.

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
содержит `linux/amd64` и `linux/arm64`. Digest относится к списку манифестов
(manifest list/index), а не к одной платформе.

Push Service использует локальный repository
`localhost:5001/tripgo-push-service`. Каталог не содержит его tag или digest:
`tripgoctl` собирает образ из встроенного бинарника и Dockerfile, отправляет его
только в управляемый registry и передаёт полученный digest генератору.
Во внешнем OCI registry образ не публикуется.

## Ports

`internal/catalog/ports.go` содержит десять точек подключения в фиксированном
порядке. Для работы `N`:

```text
hostPort = 20000 + N*1000 + hostOffset
nodePort = 30000 + N*100 + nodePortOffset
listen    = 127.0.0.1/TCP
```

`cluster start` будет резервировать все десять пробросов портов для каждой из
пяти работ, независимо от её состава. Управляемый registry отдельно занимает
`127.0.0.1:5001` (внутри контейнера: `5000`). Unit-тест проверяет уникальность 50
портов работ на хосте и 50 NodePort, отсутствие пересечений с портом registry и
соответствие стандартному диапазону NodePort.

## Environment defaults

`EnvironmentForLab` возвращает namespace, нормализованный список компонентов,
порты и полный упорядоченный набор переменных `.env`. Наборы накопительные:

| Lab | Components | Число env-переменных |
|---:|---|---:|
| 1 | postgres | 9 |
| 2 | + observability | 14 |
| 3 | + push | 20 |
| 4 | + redpanda | 27 |
| 5 | тот же stack + resilience/choice defaults | 42 |

Все функции каталога возвращают копии срезов, поэтому вызывающий код не может
изменить встроенное состояние.

Необязательная таблица `[env]` из `environment.toml` не входит в набор значений
по умолчанию. Парсер проверяет имена и буквальные однострочные значения,
запрещает совпадения со всеми 42 встроенными именами. Пользовательские переменные
сортируются по имени и добавляются после встроенного набора выбранной работы.

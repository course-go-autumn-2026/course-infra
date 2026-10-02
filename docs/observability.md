# Логи, трейсы и метрики

Начиная с работы 2, каждое окружение содержит собственные Collector, Loki,
Jaeger, Prometheus и Grafana:

```text
Go-приложение → OTLP → Collector → Loki       → Grafana / логи
                                → Jaeger     → Grafana / трейсы
                                → Prometheus → Grafana / метрики
```

Collector принимает push по OTLP HTTP и gRPC. Новых портов для логов нет.
Адрес HTTP уже записан в `.env` как `OTEL_EXPORTER_OTLP_ENDPOINT`, протокол —
`http/protobuf`. Для работы 2 это `http://localhost:22318`, gRPC —
`localhost:22317`, Grafana — `http://localhost:22300`. Все адреса текущей работы
показывает `tripgoctl connect`.

После обновления CLI выполните `tripgoctl environment start` в каталоге работы.
Команда добавит Loki, обновит конфигурации и дождётся готовности. Изменение
конфигурации Collector/Jaeger/Grafana/Loki вызывает rollout по checksum в pod template.
`environment.toml` и существующие OTLP-переменные менять не нужно.

## Подключение Go-приложения

Наличие `.env` не включает экспорт автоматически: приложение должно загрузить
его значения и инициализировать Logs SDK, exporter и bridge к своему логгеру.
Collector не читает stdout приложения на хосте.

Для `slog` установите зависимости **в репозитории приложения**, не инфраструктуры:

```sh
go get go.opentelemetry.io/otel/sdk/log \
  go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp \
  go.opentelemetry.io/contrib/bridges/otelslog
```

Минимальный пример отправки одной записи:

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "time"

    "go.opentelemetry.io/contrib/bridges/otelslog"
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
    sdklog "go.opentelemetry.io/otel/sdk/log"
    "go.opentelemetry.io/otel/sdk/resource"
)

func main() {
    ctx := context.Background()
    res, err := resource.New(ctx, resource.WithFromEnv(), resource.WithTelemetrySDK())
    if err != nil {
        log.Fatal(err)
    }
    exporter, err := otlploghttp.New(ctx)
    if err != nil {
        log.Fatal(err)
    }
    provider := sdklog.NewLoggerProvider(
        sdklog.WithResource(res),
        sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
    )
    slog.SetDefault(otelslog.NewLogger("trip-service", otelslog.WithLoggerProvider(provider)))
    slog.InfoContext(ctx, "trip created", "trip_id", "example")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := provider.Shutdown(shutdownCtx); err != nil {
        log.Fatal(err)
    }
}
```

Например, для работы 2 перед запуском этого примера:

```sh
export OTEL_SERVICE_NAME=trip-service
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:22318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
go run .
```

В настоящем сервере provider создаётся один раз при старте. `Shutdown` вызывается
после остановки обработчиков, с отдельным таймаутом. `otelslog` отправляет записи
через OTLP, но сам не дублирует их в stdout. Если нужен и вывод в консоль,
сохраните его отдельно в настройке логгера.

## Просмотр и связь с трейсами

Откройте Grafana → Explore → Loki и выберите подходящий интервал времени:

```logql
{service_name="trip-service"}
{service_name="trip-service"} |= "trip created"
{service_name="trip-service"} | trace_id="<trace ID>"
```

Вызовы `slog.InfoContext` / `slog.ErrorContext` с контекстом активного спана
позволяют bridge передать `trace_id` и `span_id`. Grafana содержит derived field
`TraceID` с переходом `View trace` в источник Jaeger. В примере выше активного
спана нет, поэтому идентификаторов трейса тоже нет.

В индексные labels Loki попадают только `service.name`, `service.namespace`,
`deployment.environment` и `deployment.environment.name`; точки заменяются
подчёркиваниями. Остальные атрибуты и идентификаторы остаются structured metadata.
Не превращайте `trace_id`, `trip_id`, `user_id` или `service.instance.id` в
индексные labels: это увеличивает число потоков.

## Хранение и ограничения

Grafana запрашивает 256 MiB памяти и имеет лимит 1 GiB, общий для сервера и
процессов плагинов источников данных. Слишком низкий лимит может приводить к
`OOMKilled` и периодическим `Failed to fetch` в браузере, даже если на хосте
достаточно памяти. При таких ошибках проверьте перезапуски и причину завершения
контейнера через `kubectl describe pod -n tripgo-lab-02 -l app.kubernetes.io/name=grafana`
(для другой работы замените namespace).

Jaeger хранит в памяти не более 10 000 трейсов, без PVC и дисковой базы данных.
Старые трейсы вытесняются; при 100 новых трейсах в секунду это примерно 100 секунд
истории, а не фиксированный TTL. Перезапуск Jaeger и `environment stop/start`
очищают историю. Запрос памяти — 64 MiB, лимит контейнера — 256 MiB,
`GOMEMLIMIT` — 160 MiB. Лимит числа трейсов не ограничивает размер каждого из них,
а мягкий лимит Go не заменяет лимит контейнера. Батчи ограничены 1024 спанами.

Loki работает одной репликой с файловым хранилищем на PVC 1 GiB. Индекс, chunks,
WAL и данные compactor сохраняются на одном томе. WAL восстанавливает принятые
данные после перезапуска; при штатной остановке Loki также сбрасывает chunks.
Compactor удаляет записи старше семи дней с задержкой цикла обработки и удаления.
Retention ограничивает возраст, а не размер: при большом потоке логов следите за
свободным местом; в kind размер local-path PVC не является жёсткой дисковой квотой.

`environment stop/start` сохраняет данные Loki; `environment reset` удаляет их
вместе с namespace. Источники Grafana и Collector обращаются только к сервисам
своего namespace. Loki доступен лишь через внутренний ClusterIP, без NodePort.

Это локальный учебный стенд: Grafana разрешает анонимный Admin, Loki — без auth.
Не публикуйте эти endpoints в интернет. Для production нужны аутентификация,
объектное хранилище и отдельный план отказоустойчивости.

Очереди SDK и экспортёра Collector находятся в памяти. У экспортёра Loki очередь
ограничена 256 запросами, временные ошибки повторяются до 5 минут. Переполнение,
неповторяемые ошибки или аварийное завершение Collector/приложения могут потерять
ещё не доставленные записи; подтверждение OTLP не гарантирует запись в Loki.

## Проверки

```sh
go test -race ./...
make integration-isolation
```

Вторая команда требует отсутствующего `tripgo-local` и изменяет Docker/kind.
Она проверяет HTTP/gRPC push, запросы через Grafana, изоляцию работ 2/4/5,
сохранность логов после stop/start и замены pod, очистку трейсов при stop/start
и удаление логов при reset.
Для ручной проверки OTLP/gRPC в уже запущенной работе 2:

```sh
go run ./tests/integration/otlp-grpc-client \
  -endpoint localhost:22317 -service trip-service -span grpc-smoke
```

Команда отправляет связанный трейс и лог; запись `grpc-smoke` появится в Loki,
трейс — в Jaeger. Трейсы хранятся только в памяти и не переживают перезапуск Jaeger.

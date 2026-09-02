# Cluster lifecycle v1

Статус: реализация этапа 4, 2026-09-01.

## Owned topology

```text
Docker
├── tripgo-local-registry
│   ├── image: registry:2.8.3@sha256:a3d8...
│   ├── host: 127.0.0.1:5001 → container:5000
│   └── labels: managed-by + cluster-id
└── tripgo-local-control-plane
    ├── kind v0.27.0 / Kubernetes v1.32.2
    ├── 50 loopback extraPortMappings
    ├── /etc/tripgoctl/cluster-id
    └── containerd mirror:
        localhost:5001 → http://tripgo-local-registry:5000
```

Registry подключается к Docker network `kind` после создания cluster. Runtime
integration проверяет, что image, запушенный в `localhost:5001`, успешно
запускается в pod по локальному digest.

## Helper supply chain

`kind` скачивается только с GitHub release v0.27.0 и кешируется после SHA-256
verification:

| Platform | SHA-256 |
|---|---|
| darwin/amd64 | `3435134325b6b9406ccfec417b13bb46a808fc74e9a2ebb0ca31b379c8293863` |
| darwin/arm64 | `5240ca1acb587e1d0386532dd8c3373d81f5173b5af322919fc56f0cdd646596` |
| linux/amd64 | `a6875aaea358acf0ac07786b1a6755d08fd640f4c79b7a2e46681cc13f49a04b` |
| linux/arm64 | `5e4507a41c69679562610b1be82ba4f80693a7826f4e9c6e39236169a3e4f9d0` |

Повреждённый cache entry удаляется и скачивается заново. Download выполняется во
временный файл, затем `fsync`, mode `0700` и atomic rename.

## Start

`cluster start` под operation lock:

1. проверяет platform, Docker CLI/daemon;
2. получает verified kind;
3. при существующем cluster сверяет state, kind-config hash, marker и registry
   labels и возвращает idempotent success;
4. для нового cluster проверяет сразу registry port + 50 lab ports;
5. предупреждает при Docker memory < 6 GiB или disk free < 10 GiB;
6. создаёт registry только по pinned digest;
7. создаёт kind с pinned node image и ожиданием readiness 120 секунд;
8. подключает registry к network, записывает node marker, API-visible identity
   ConfigMap и atomic state.

При ошибке после начала mutation созданные cluster/registry удаляются.

## Status и stop

State находится в OS user config directory, mode `0600`, и содержит schema,
cluster identity, версии, SHA-256 kind config и известные namespace. Cluster и
environment mutations используют один advisory `flock`; environment operation
удерживает lease до Kubernetes mutation и state commit. Kubernetes client
получает kubeconfig непосредственно через verified kind, а не из текущего
пользовательского context, и сверяет API identity ConfigMap.

`status` ничего не меняет и требует совпадения node marker, registry labels и
state. `stop` требует `yes` либо `--yes`, повторно проверяет ownership перед
удалением и удаляет kind + registry. Отсутствующий cluster — idempotent success;
foreign/mismatched resources возвращают exit code 4.

## Проверки итерации

- unit/race: downloader, checksum repair, state, operation lock, config с 50
  mappings, lifecycle state machine, Docker unavailable, multi-port conflicts;
- реальное создание, status и повторный start;
- реальный registry push → kind pull by digest;
- NodePort integration через по одному mapping каждого lab:
  `21081`, `22081`, `23081`, `24081`, `25081`;
- реальный stop, повторный stop и конфликт `127.0.0.1:5001` с exit code 4.

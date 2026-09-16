# course-infra

Локальная инфраструктура лабораторных работ TripGo.

## Установка

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL \
  https://github.com/course-go-autumn-2026/course-infra/releases/latest/download/install-tripgoctl \
  | sh
```

Поддерживаются macOS и Linux (`amd64`/`arm64`).
Для работы инфраструктуры нужен запущенный Docker.

## Документация

[Требования, использование, сборка и разработка](docs/README.md).

[Лицензия MIT](LICENSE).

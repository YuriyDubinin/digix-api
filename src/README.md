# digix-api

## Архитектура

Слоистая (Clean Architecture):

- `internal/domain` — сущности, доменные ошибки, интерфейсы репозиториев. Не зависит от других слоёв.
- `internal/repository/postgres` — реализация интерфейсов из `domain` поверх PostgreSQL (pgx/v5).
- `internal/service` — бизнес-логика: оркестрация репозиториев, доменная валидация.
- `internal/transport/http` — HTTP handlers, middleware, роутинг (chi), DTO.
- `internal/config` — загрузка и валидация конфигурации из окружения.
- `pkg/logger` — обёртка над `log/slog`.
- `migrations` — SQL-миграции (golang-migrate).
- `cmd/api` — точка входа.

## Требования

- Go 1.23+
- Docker
- Docker Compose v2

## Как запустить

Все команды выполняются из каталога `src/`.

1. Подготовить окружение:

   ```sh
   cp .env.example .env
   ```

2. Поднять PostgreSQL:

   ```sh
   make docker-up
   ```

3. Накатить миграции (после того как будет установлен `golang-migrate`):

   ```sh
   make migrate-up
   ```

4. Запустить сервис локально:

   ```sh
   make run
   ```

## Полезные команды

| Команда              | Что делает                                  |
| -------------------- | ------------------------------------------- |
| `make run`           | Запустить сервис локально                   |
| `make build`         | Собрать бинарь в `bin/api`                  |
| `make test`          | Запустить тесты с race detector             |
| `make lint`          | `go vet` + `golangci-lint` (если установлен)|
| `make migrate-up`    | Накатить миграции                           |
| `make migrate-down`  | Откатить одну миграцию                      |
| `make migrate-create name=...` | Создать пару `up/down` файлов     |
| `make docker-up`     | Поднять docker-compose                      |
| `make docker-down`   | Остановить docker-compose                   |
| `make docker-logs`   | Логи docker-compose                         |
| `make docker-rebuild`| Пересобрать и поднять заново                |

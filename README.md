# Digix API

REST API на Go. Сервис работает в Docker-контейнере и подключается к PostgreSQL,
которая поднимается ОТДЕЛЬНО (своим docker-compose в другом проекте).

## Запуск

1. Убедись, что PostgreSQL уже запущена и доступна на `localhost:5432` с реквизитами
   `my_user / my_pass / my_db`. Этот сервис не поднимает БД.
2. Подними сервис:
   ```bash
   make docker-up
   # или: docker compose up -d --build
   ```
3. Проверь логи — должна быть строчка `ready`:
   ```bash
   make docker-logs
   ```
   Ожидаемые строки: `database connected`, `migrations applied` (или
   `no pending migrations`), `ready`.

Остановить и пересобрать:

```bash
make docker-down       # стоп
make docker-rebuild    # пересобрать и поднять
```

## Подключение к БД

В `docker-compose.yml` `POSTGRES_HOST=host.docker.internal`. Это работает потому, что:

- В секции `extra_hosts` указано `host.docker.internal:host-gateway` — на Linux это
  явно прокидывает шлюз хоста в контейнер, чтобы имя резолвилось в IP хоста.
  На macOS/Windows Docker Desktop делает это автоматически, но строка не мешает.

Если БД крутится **не на хосте**, а на удалённом сервере — поменяй
`POSTGRES_HOST` на IP/домен и удали блок `extra_hosts`.

**Заметка.** Если в будущем понадобится объединить этот сервис и compose БД в
один кластер — создай в проекте БД внешнюю сеть (`docker network create
shared-net`), подключи к ней оба compose-файла через
`networks: { default: { name: shared-net, external: true } }` и поставь
`POSTGRES_HOST` равным имени сервиса БД (`postgres`). В таком сценарии
`extra_hosts` не нужны.

## Миграции

Используется [golang-migrate](https://github.com/golang-migrate/migrate).
Внутри сервиса миграции применяются автоматически при старте. CLI нужен только
для ручного управления (создание новой миграции, ручной up/down).

Установка CLI:

```bash
# macOS
brew install golang-migrate

# Linux (бинарь)
curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz \
  | tar xvz
sudo mv migrate /usr/local/bin/

# Go install
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Команды:

```bash
make migrate-create name=add_something   # создать новую миграцию
make migrate-up                          # применить все
make migrate-down                        # откатить последнюю
```

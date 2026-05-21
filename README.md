# Digix API

## Миграции

Используется [golang-migrate](https://github.com/golang-migrate/migrate). CLI нужен для целей `make migrate-*`.

Установка:

```bash
# macOS
brew install golang-migrate

# Linux (бинарь)
curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xvz
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

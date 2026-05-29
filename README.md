# Dijex API

## Сборка
```
docker build --platform linux/amd64 -t yuriydubinin100/dijex-api:1.0.0 .
```

## Запуск
```
docker run -d \
  --name dijex-api \
  --env-file .env \
  -p 18080:8080 \
  --user root \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /run/systemd:/run/systemd:ro \
  -v /run/dbus/system_bus_socket:/run/dbus/system_bus_socket:ro \
  -v dijex-ssh:/data/ssh \
  -v /usr/libexec/docker/cli-plugins/docker-compose:/usr/libexec/docker/cli-plugins/docker-compose:ro \
  yuriydubinin100/dijex-api:1.0.0
```

## Деплой
```
docker push yuriydubinin100/dijex-api:1.0.0
```

```
docker pull yuriydubinin100/dijex-api:1.0.0
```

## Эндпоинты

Базовый URL при локальном запуске: `http://localhost:18080`.

Защищённые эндпоинты требуют заголовок `Authorization: Bearer <token>` (токен из `POST /api/auth/login`).

### Публичные

| Метод | Путь | Что делает |
|---|---|---|
| `GET` | `/api/ping` | Health-check сервиса (200 OK, если сервис жив). |
| `POST` | `/api/feedbacks/requests` | Приём заявок с лендинга; отправляет уведомление в Telegram. |
| `POST` | `/api/auth/login` | Логин сотрудника по email + паролю. Возвращает Bearer-токен. |

### Авторизация и профиль (защищённые)

| Метод | Путь | Что делает |
|---|---|---|
| `POST` | `/api/auth/logout` | Отзывает текущий Bearer-токен (logout). |
| `GET` | `/api/me` | Данные текущего залогиненного сотрудника (id, role, status). |

### Система (защищённые)

| Метод | Путь | Что делает |
|---|---|---|
| `GET` | `/api/system/main` | Подробный снимок состояния сервера: app, host (включая `country_code`/`country` по публичному IP), cpu, memory, disks, network, process, database, версии Docker и Docker Compose. |
| `GET` | `/api/system/containers` | Список Docker-контейнеров хоста с тегами, статусом, портами, сетями, лимитами. |
| `GET` | `/api/system/services` | Список systemd-сервисов хоста: статусы, PID, память, CPU, перезапуски. |

### SSH-ключ приложения (защищённые)

| Метод | Путь | Что делает |
|---|---|---|
| `GET` | `/api/system/ssh/check` | Строгая проверка: есть ли файл ключа И валиден ли он. 200 / 404 / 422. |
| `GET` | `/api/system/ssh/get` | Получить публичный ключ (для копирования в `authorized_keys` серверов). 404, если ключа нет. |
| `POST` | `/api/system/ssh/create` | Создаёт Ed25519 ключ в стандартном месте (идемпотентно, не перезаписывает существующий). |
| `DELETE` | `/api/system/ssh/delete` | Удаляет файл приватного ключа и `.pub`. |

### Docker Registries (защищённые)

| Метод | Путь | Что делает |
|---|---|---|
| `POST` | `/api/registries/create` | Создать подключение к Docker Registry (создаётся выключенным). |
| `GET` | `/api/registries/list` | Список подключений с пагинацией, фильтрами и сортировкой. |
| `PUT` | `/api/registries/update` | Полное обновление подключения по id. |
| `DELETE` | `/api/registries/delete` | Мягкое удаление (soft-delete через `deleted_at`). |
| `POST` | `/api/registries/connect` | Проверить сохранённое подключение по id (логин в аккаунт по email). При успехе — активирует запись. |
| `POST` | `/api/registries/ping` | Проверить подключение по сохранённому id и переключить `is_active` (успех → активна, провал → неактивна). |
| `POST` | `/api/registries/images` | Список образов (репозиториев) с тегами и метаданными по сохранённому подключению. |

### Серверы (защищённые)

| Метод | Путь | Что делает |
|---|---|---|
| `POST` | `/api/servers/create` | Создать запись о сервере (host/port/протокол/креды/окружение/теги). |
| `GET` | `/api/servers/list` | Список серверов с пагинацией, фильтрами (env/protocol/auth_method/is_active) и поиском по name/host. |
| `PUT` | `/api/servers/update` | Полное обновление сервера по id (секреты управляются `null`/`""`/значением). |
| `DELETE` | `/api/servers/delete` | Мягкое удаление (soft-delete). |
| `POST` | `/api/servers/remote/connect` | SSH-вход на сервер (наш ключ → пароль), проверка сессии, сбор фактов (os, kernel, arch, cpu, hostname, публичный IP и страна) в БД. |
| `POST` | `/api/servers/remote/ping` | SSH health-check сохранённого сервера; переключает `is_active` в обе стороны (успех → true, провал → false). |
| `POST` | `/api/servers/remote/install-ssh` | Заходит по паролю и идемпотентно ставит наш публичный ключ в `~/.ssh/authorized_keys`, верифицирует ключ переподключением. При успехе выставляет `ssh_key_installed=true` и переключает `auth_method` на `PRIVATE_KEY`. |
| `POST` | `/api/servers/remote/system/main` | Подробный снимок удалённого сервера через SSH: host, cpu, memory, disks, network, docker. JSON-контракт идентичен `/api/system/main`, фронт может рендерить теми же компонентами. |
| `POST` | `/api/servers/remote/system/containers` | Список Docker-контейнеров удалённого сервера через SSH (`docker version` + `info` + `inspect --size`). JSON-контракт идентичен `/api/system/containers`. |
| `POST` | `/api/servers/remote/system/services` | Список systemd-сервисов удалённого сервера через SSH (`systemctl list-units` + `show`). JSON-контракт идентичен `/api/system/services`. |

## Геолокация по IP

Поля `country_code` (ISO 3166-1 alpha-2) и `country` (англ. имя) определяются по публичному IP **локально**, без внешних сетевых вызовов:

- для самого API-хоста — по `host.public_ip` в ответе `GET /api/system/main`;
- для удалённых серверов — по IP, который сервер видит наружу (best-effort `curl https://api.ipify.org` или `dig myip.opendns.com` через SSH-сессию во время `POST /api/servers/remote/connect`); резолв выполняется тем же резолвером в сервисе.

Источник данных — [DB-IP Lite Country](https://db-ip.com/db/lite.php), лицензия [CC-BY 4.0](https://creativecommons.org/licenses/by/4.0/). Файл базы `internal/geo/data/dbip-country-lite.mmdb` встроен в бинарь через `//go:embed`. Обновлять вручную раз в несколько месяцев:

```bash
curl -sL "https://download.db-ip.com/free/dbip-country-lite-$(date -u +%Y-%m).mmdb.gz" \
  | gunzip > internal/geo/data/dbip-country-lite.mmdb
```

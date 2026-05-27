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

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/ping` | Проверка, что сервис жив. Возвращает `200 {"status":"ok","message":"API is up and running"}`. |
| `POST` | `/api/feedbacks/requests` | Создание заявки обратной связи. Принимает JSON, сохраняет в БД, возвращает `201` с `id`, `status`, `created_at`. |

### `POST /api/feedbacks/requests`

**Headers:** `Content-Type: application/json`

**Body:**

| Поле | Тип | Обязательное | Ограничения |
|---|---|---|---|
| `name` | string | да | 2–255 символов |
| `email` | string | да | валидный email, до 255 символов |
| `phone` | string | нет | до 50 символов |
| `subject` | string | нет | до 500 символов |
| `message` | string | да | 10–5000 символов |

**Пример запроса:**
```json
{
  "name": "Иван Петров",
  "email": "ivan@example.com",
  "phone": "+7 999 123-45-67",
  "subject": "Хочу заказать сайт",
  "message": "Здравствуйте, интересует разработка корпоративного сайта."
}
```

**Успех — `201 Created`:**
```json
{
  "id": "ab12cd34-...",
  "status": "new",
  "created_at": "2026-05-21T12:34:56Z"
}
```

**Ошибки:**

- `400 INVALID_JSON` — невалидный JSON или лишние поля.
- `422 VALIDATION_ERROR` — нарушены правила валидации. В `details` — список полей с проблемами.
- `500 INTERNAL_ERROR` — внутренняя ошибка (смотри `docker logs dijex-api`).


# EventScope — Продуктовая аналитика

Проект №06. Самостоятельное Go-приложение. Сборка: `./cmd/eventscope`.
Общие пакеты находятся в `internal/platform`, поэтому сохраняйте структуру репозитория.

## Реализовано в v0.1

Приём до 500 событий за запрос; дедупликация по event id; проверка конфликта содержимого; дневные метрики; двухшаговая воронка; когортное удержание; экспорт в ClickHouse; Kafka outbox.

Данные: `analytics_events; ClickHouse: events`. Схема: `../../migrations/eventscope_001.sql`.
Все записи изолированы по tenant; роль определяется серверным API-токеном.

## Запуск

Из корня репозитория:

```bash
python3 scripts/setup.py  # один раз для всего набора
PROJECT=eventscope docker compose up --build -d app
python3 scripts/demo.py eventscope
```

Для JobRunner запускайте ещё `--profile jobs`; точные команды профилей — в корневом README.
Демо создаёт тестовые записи. Остановить: `docker compose down`.

## API

Все пути требуют `Authorization: Bearer <token>`. Ошибки возвращаются как JSON.
`Idempotency-Key` необязателен, но рекомендуется для команд создания и отправки.
Повтор того же запроса с тем же ключом возвращает сохранённый ответ; изменение запроса даёт 409.

| Метод | Путь |
|---|---|
| POST | `/events` |
| GET | `/analytics/daily` |
| GET | `/analytics/funnel` |
| GET | `/analytics/retention` |

## Примеры тел запросов

`POST /events`:

```json
{
  "events": [
    {
      "id": "event-1",
      "user_id": "alice",
      "name": "view",
      "occurred_at": "2026-01-01T10:00:00Z",
      "properties": {}
    }
  ]
}
```

## Проверка

```bash
TEST_DATABASE_URL='postgres://portfolio:portfolio-local-only@localhost:5432/portfolio?sslmode=disable' \
  go test -count=1 -v ./tests -run '^TestEventScope$'
```

Интеграционные тесты пишут данные в указанную БД. Используйте отдельную тестовую базу.
Полный сценарий проверки находится в `../../tests/integration_test.go`.

## Границы версии и дальнейшая работа

Многошаговые воронки, версии схем событий, пакетный высокопроизводительный экспорт. HTTP-отчёты сейчас считаются в PostgreSQL; ClickHouse доступен отдельными SQL-запросами.

Это реализованное ядро проекта, а не заявление о завершении всех расширений исходной идеи.
Подробнее об ограничениях окружения проверки — `../../VALIDATION.md`.

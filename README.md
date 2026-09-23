# nado

Скелет веб-приложения на Go: HTML-страницы, JSON API и Microsoft SQL Server.

## Структура

```
cmd/nado/            точка входа: перехват сигналов, запуск app
internal/
  app/               сборка зависимостей и жизненный цикл (старт → работа → остановка)
  config/            конфигурация из переменных окружения и .env
  database/          пул соединений к SQL Server, транзакции, маппинг ошибок
  handler/api/       JSON-обработчики (users, health)
  handler/web/       обработчики HTML-страниц
  httpx/             транспортный слой: ошибки, ответы, middleware, валидация
  model/             доменные сущности
  repository/        SQL-запросы (единственное место, где есть SQL)
  router/            дерево маршрутов и цепочка middleware
  service/           бизнес-логика
  view/              движок шаблонов (layout + partials + страницы)
web/
  templates/         layouts/, partials/, pages/
  static/            css, js, изображения
migrations/          SQL-скрипты схемы
```

Поток запроса: `router → middleware → handler → service → repository → SQL Server`,
ответ — JSON (`httpx`) или HTML (`view`).

## Запуск

```powershell
Copy-Item .env.example .env      # и заполнить доступы к БД
sqlcmd -S localhost -d nado -U sa -P '<пароль>' -i migrations/0001_init.sql
go run ./cmd/nado
```

Открыть `http://localhost:8080` — страницы, `http://localhost:8080/api/v1/users` — API.

Сборка одного файла со встроенными шаблонами и статикой:

```powershell
go build -trimpath -ldflags "-X main.version=$(git describe --tags --always)" -o bin/nado.exe ./cmd/nado
```

Тесты: `go test -race ./...` (проходят без БД — репозиторий подменяется заглушкой).

## Конфигурация

Все параметры — переменные окружения, полный список с комментариями в `.env.example`.
Реальное окружение имеет приоритет над `.env`. Ключевое:

| Переменная | Значение |
|---|---|
| `APP_DEBUG` | `true` — шаблоны читаются с диска на каждый запрос, логи текстом |
| `APP_ENV` | `prod` включает JSON-логи, HSTS, паузу дренирования и строгую проверку конфига |
| `HTTP_SHUTDOWN_TIMEOUT` | сколько ждать завершения активных запросов при остановке |
| `DB_QUERY_TIMEOUT` | таймаут по умолчанию для каждого SQL-запроса |
| `DB_MAX_OPEN_CONNS` | размер пула; должен быть не меньше `DB_MAX_IDLE_CONNS` |

## Безопасное завершение

По `SIGINT`/`SIGTERM` (`signal.NotifyContext` в `cmd/nado/main.go`):

1. `/readyz` начинает отдавать `503` — балансировщик уводит трафик;
2. пауза `drainDelay` (только в prod) — чтобы он успел это заметить;
3. `server.Shutdown` — новые соединения не принимаются, активные запросы
   дорабатывают в пределах `HTTP_SHUTDOWN_TIMEOUT`, при превышении — `server.Close`;
4. закрывается пул соединений с БД — строго после того, как запросы завершились.

Повторный `Ctrl+C` убивает процесс немедленно: `NotifyContext` снимает перехват
после первого сигнала.

## Работа с базой

* Драйвер — `github.com/microsoft/go-mssqldb`, пул `database/sql` с ограничениями
  из конфига, `Ping` при старте: неверные доступы видны сразу, а не на первом запросе.
* Все параметры передаются через `sql.Named` (`WHERE id = @id`) — конкатенация
  пользовательского ввода в текст запроса недопустима.
* Каждый запрос получает контекст с таймаутом (`db.Context(ctx)`), поэтому зависший
  запрос не держит соединение из пула.
* Транзакции — `db.WithTx`: коммит при успехе, откат при ошибке и при панике.
* Ошибки драйвера приводятся к `database.ErrNotFound` / `database.ErrConflict`
  (`MapError`), сервисы работают с ними через `errors.Is`, не зная про коды SQL Server.

Пример транзакции — `repository.UserRepository.CreateWithAudit`.

## API

| Метод | Путь | Назначение |
|---|---|---|
| `GET` | `/healthz` | процесс жив |
| `GET` | `/readyz` | готов принимать трафик (проверяет БД) |
| `GET` | `/api/v1/users?page=&per_page=&search=&status=` | список с пагинацией |
| `POST` | `/api/v1/users` | создание |
| `GET` | `/api/v1/users/{id}` | карточка |
| `PUT` | `/api/v1/users/{id}` | изменение |
| `DELETE` | `/api/v1/users/{id}` | удаление |

Ответы в одном формате: `{"data": ..., "meta": ...}` или
`{"error": {"code", "message", "fields", "request_id"}}`. Внутренние причины ошибок
попадают только в лог — клиенту уходит `request_id`, по которому их можно найти.

Новый ресурс добавляется так: `repository` (SQL) → `service` (правила) →
`handler/api` (`Routes()`) → строка `r.Mount(...)` в `internal/router/router.go`.

## Страницы и шаблоны

Страница собирается из трёх частей:

```
web/templates/layouts/base.gohtml     каркас: <html>, <head>, общая разметка
web/templates/partials/*.gohtml       переиспользуемые блоки: header, footer, alert, pagination
web/templates/pages/<name>.gohtml     содержимое страницы: {{ define "content" }}
```

Переменные передаются через `view.Data`:

```go
data := view.NewData(r, "Пользователи").
    With("Users", users).
    With("Total", total)

return h.render.Render(w, http.StatusOK, "users", data) // pages/users.gohtml
```

В шаблоне — `{{ .Users }}`, `{{ .Total }}`. Переменные из `view.WithGlobals`
(`AppName`, `Env`, `Version`) доступны на каждой странице без явной передачи.

Подключение шаблона из другого файла:

```gotemplate
{{ template "header" . }}                                  {{/* весь набор переменных */}}
{{ template "alert" dict "Kind" "error" "Text" .Message }} {{/* только нужные */}}
```

Функция `dict` собирает набор переменных прямо в шаблоне, поэтому partial можно
переиспользовать с разными данными. Partial-ы могут вызывать друг друга
(`header` → `nav-link`). Второй layout выбирается явно:
`render.RenderLayout(w, status, "minimal", "error", data)`.

Доступные функции: `dict`, `default`, `formatDate`, `now`, `year`, `upper`, `lower`,
`add`, `sub`, `safeHTML`, `safeAttr`. Свои добавляются через `view.WithFuncs`.

Рендеринг идёт в буфер и только потом в ответ — ошибка в шаблоне не оставит
клиенту наполовину отрисованную страницу со статусом `200`. При старте
компилируются все сочетания layout×страница (`Warmup`): опечатка в шаблоне
валит приложение сразу, а не в проде на запросе пользователя.

## Безопасность

* Экранирование по умолчанию (`html/template`); `safeHTML` — только для доверенного
  содержимого.
* Заголовки `CSP`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`
  на HTML-маршрутах, `HSTS` — в prod.
* Тело JSON-запроса ограничено 1 МиБ, неизвестные поля отклоняются, входные данные
  проверяются тегами `validate` (`go-playground/validator`).
* Таймауты HTTP-сервера защищают от медленных клиентов; `middleware.Timeout`
  ограничивает время обработки запроса.
* Паника в обработчике не роняет процесс: `httpx.Recoverer` логирует стек и
  отдаёт `500`.
* В prod конфигурация не пускает пустой пароль БД и `DB_TRUST_SERVER_CERTIFICATE=true`.

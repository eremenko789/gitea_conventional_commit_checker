# gitea_conventional_commit_checker

Микросервис для **Gitea**: по вебхукам `pull_request` загружает коммиты PR из API, проверяет первую строку сообщения на [Conventional Commits 1.0.0](https://www.conventionalcommits.org/) и выставляет [commit status](https://docs.gitea.com/api/) через `POST /api/v1/repos/{owner}/{repo}/statuses/{sha}`.

Архитектура близка к описанной в ТЗ референсу [gitea_jenkins_integ](https://github.com/eremenko789/gitea_jenkins_integ): вебхук → очередь → пул воркеров → вызовы Gitea API.

## События и действия PR

Обрабатывается заголовок `X-Gitea-Event` / `X-Gogs-Event` = `pull_request` и действия **`opened`**, **`reopened`**, **`synchronize`** (обновление ветки PR). Остальные действия (например `closed`, `labeled`, правки только заголовка без синхронизации ветки) **игнорируются** с ответом `200`, чтобы не создавать лишнюю нагрузку; при появлении в Gitea отдельных событий вроде `edited` они по умолчанию также не запускают проверку.

## Быстрый старт

```bash
cp config.example.yaml config.yaml
# заполните gitea.base_url, gitea.token, server.webhook_secret, repositories

go run ./cmd/webhook-service -config config.yaml
```

Проверка здоровья:

```bash
curl -sS http://127.0.0.1:8080/healthz
# ok
```

В Gitea для репозитория создайте вебхук: URL `http(s)://<хост>:8080/webhook`, событие **Pull Request**, секрет совпадает с `server.webhook_secret`.

## Конфигурация и шаблоны

См. `config.example.yaml`. Поля `description_success`, `description_failure` и опционально `description_pending` / `target_url_template` — это шаблоны Go [`text/template`](https://pkg.go.dev/text/template).

Доступные поля шаблона:

| Поле | Описание |
|------|----------|
| `.PRNumber` | Номер PR (index) |
| `.PRTitle` | Заголовок PR |
| `.RepoFullName` | `org/repo` |
| `.Owner`, `.Repo` | Владелец и имя репозитория |
| `.InvalidCommits` | Многострочный текст `shortSHA: subject` по невалидным коммитам |
| `.InvalidCommitsList` | Срез структур с полями `ShortSHA`, `FullSHA`, `Subject` |
| `.BadCount`, `.GoodCount`, `.TotalChecked` | Счётчики |

Итоговый `description` после рендеринга не должен быть пустым — это проверяется **при старте** сервиса (включая эффективный конфиг для каждого репозитория из списка).

## Поведение статусов

- Итог всегда пишется на **HEAD** PR (`pull_request.head.sha`).
- Если `check.status_on_each_commit: true`, тот же `state` и `description` дублируются на **каждый** SHA из списка коммитов PR (без дубликата HEAD).
- Нарушение правил → `failure` (не `error`).
- Ошибки Gitea API / сеть после ретраев → по возможности один статус `error` на HEAD с нейтральным текстом (без утечки секретов).

## Подпись вебхука

Проверяются `X-Gitea-Signature` (hex HMAC-SHA256 тела) и запасной вариант `X-Hub-Signature-256` (`sha256=<hex>`), как в исходнике Gitea. Если `webhook_secret` **пустой**, подпись **не** проверяется (только для отладки).

## Токен Gitea

Нужны права на чтение репозитория/PR и создание commit status для целевых репозиториев (формулировка scope зависит от версии Gitea; обычно достаточно классического токена с доступом к репозиторию).

## Сборка и контейнер

```bash
make build
make test
make docker
docker compose up --build
```

## Белый список репозиториев

События по репозиториям, не перечисленным в `repositories`, получают **HTTP 200** и не ставятся в очередь.

## Очередь

При переполнении очереди вебхук отвечает **503**; клиент Gitea обычно повторит доставку.

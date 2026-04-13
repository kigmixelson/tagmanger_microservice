# tagmanager_microservice

Go-микросервис для работы с коллекцией тегов в MongoDB.

## Конфигурация

Сервис читает MongoDB URL из JSON-файла `/etc/saymon/saymon-server.conf`:

```json
{
  "db": {
    "mongodb": {
      "url": "mongodb://127.0.0.1:27017/saymon?w=1"
    }
  }
}
```

Переменные окружения:

- `HTTP_ADDR` - адрес HTTP сервера (по умолчанию `:3838`)
- `SAYMON_CONFIG_PATH` - путь к конфигу (по умолчанию `/etc/saymon/saymon-server.conf`)
- `TAGS_COLLECTION` - имя коллекции (по умолчанию `tags`)
- `DEBUG_REQUEST_LOGS` - подробные логи входящих запросов и pre-check `/node/api/users/current` (`false` по умолчанию)

## API

### Healthcheck

`GET /healthz`

- `200` и `{"status":"ok"}` если сервис жив и MongoDB доступна
- `503` и `{"status":"unhealthy","error":"mongodb unavailable"}` если MongoDB недоступна

### Version

`GET /version`

Возвращает версию приложения и дату сборки:

```json
{
  "version": "1.0.14",
  "buildDate": "2026-04-13T14:02:55Z"
}
```

### Авторизация запросов

Перед обработкой каждого запроса к `/api/tags...` сервис делает проверку:

- `GET /node/api/users/current` на тот же `hostname`, который пришел в исходном запросе;
- схема (`http`/`https`) для исходящего запроса определяется по reverse-proxy заголовкам (`X-Forwarded-Proto`, `Forwarded`, а также типовые `X-Forwarded-Ssl`/`Front-End-Https`), потому что до самого сервиса соединение часто идет по HTTP даже если клиент заходит по HTTPS;
- если reverse-proxy заголовки отсутствуют, используется TLS на соединении с сервисом (`r.TLS`) и затем `r.URL.Scheme`;
- в запрос проверки прокидываются:
  - header `x-csrf-token`;
  - cookie `sid` и `csrf`.

Правила доступа:

- операции чтения (`GET /api/tags`, `GET /api/tags/search`) разрешены, если `/node/api/users/current` вернул непустой `id`; для чтения `x-csrf-token` не обязателен;
- операции записи (`POST`, `PUT`, `PATCH`) требуют `x-csrf-token` (header) и cookie `csrf`, а также permission `manage-configuration`;
- при отказе в проверке или недостатке прав сервис возвращает `403`.

### 1) Получить всю коллекцию

`GET /api/tags`

### 2) Получить tag по ObjectID

`GET /api/tags?id=<object_id_hex>`

### 3) Получить tag по имени

`GET /api/tags?name=<name>`

### 4) Обновить tag по id

`PATCH /api/tags/<object_id_hex>`

Body пример:

```json
{
  "name": "Индикатор",
  "color": "#a93d3d",
  "public": "off"
}
```

### 5) Создать tag (POST и PUT)

`POST /api/tags`  
`PUT /api/tags`

Body пример:

```json
{
  "name": "test626",
  "color": "#e53e3e",
  "public": "off",
  "class": "33",
  "description": "ddd",
  "att2": "vv"
}
```

### 6) Поиск по серверу

`GET /api/tags/search?q=<text>&fields=name,description,color&limit=50`

- `q` - обязательный поисковый текст
- `fields` - список полей через запятую (необязательный). По умолчанию: `name,description,color,class,public,visibility`
- `limit` - ограничение количества записей, от `1` до `200` (по умолчанию `50`)

## Запуск в Docker

Сборка:

```bash
docker build -t tagmanager:latest .
```

Если нужно пересобрать образ полностью без использования кэша слоев:

```bash
docker build --no-cache -t tagmanager:latest .
```

Оптимизированная сборка для production (принудительное обновление базовых слоев и целевая платформа):

```bash
docker build --pull --platform linux/amd64 -t tagmanager:latest .
```

Текущий `Dockerfile` уже использует multi-stage сборку и запускает приложение от непривилегированного пользователя (`appuser`), поэтому отдельная дополнительная "промежуточная" схема здесь не даст заметного выигрыша по безопасности или ресурсам.

Запуск:

```bash
docker run --rm \
  -p 3838:3838 \
  -e HTTP_ADDR=:3838 \
  -e SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf \
  -e TAGS_COLLECTION=tags \
  -e DEBUG_REQUEST_LOGS=false \
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
```

Для отладки можно включить подробные логи:

```bash
-e DEBUG_REQUEST_LOGS=true
```

Или через docker compose:

```bash
docker compose up -d --build
```

## Доставка готового образа в runtime (tar-ball)

### 1) Собрать и упаковать образ на build-сервере

```bash
docker build --pull --platform linux/amd64 -t tagmanager:latest .
docker save tagmanager:latest | gzip > tagmanager_latest.tar.gz
```

или лучше

```bash
docker build --pull --platform linux/amd64  --no-cache -t tagmanager:latest .
docker save tagmanager:latest | gzip > tagmanager_latest.tar.gz
```


### 2) Передать tar-ball в runtime окружение

Пример через `scp`:

```bash
scp tagmanager_latest.tar.gz user@runtime-host:/tmp/
```

### 3) Загрузить образ на runtime-host

```bash
gzip -dc /tmp/tagmanager_latest.tar.gz | docker load
```

### 4) Запустить контейнер из загруженного образа

```bash
docker run -d --name tagmanager \
  --restart unless-stopped \
  -p 3838:3838 \
  -e HTTP_ADDR=:3838 \
  -e SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf \
  -e TAGS_COLLECTION=tags \
  -e DEBUG_REQUEST_LOGS=false \
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
```

Проверка после запуска:

```bash
curl http://127.0.0.1:3838/healthz
```

## Troubleshooting: MongoDB на localhost при запуске в Docker

Если в `/etc/saymon/saymon-server.conf` указан URL вида:

```json
{
  "db": {
    "mongodb": {
      "url": "mongodb://localhost:27017/saymon?w=1"
    }
  }
}
```

и контейнер падает с ошибкой подключения к MongoDB (`connect: connection refused`), причина в том, что `localhost` внутри контейнера указывает на сам контейнер, а не на хост.

Для Linux runtime-хостов используйте host network:

```bash
docker run --rm \
  --network host \
  -e SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf \
  -e TAGS_COLLECTION=tags \
  -e DEBUG_REQUEST_LOGS=false \
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
```

Примечания:

- при `--network host` параметр `-p 3838:3838` не нужен;
- в этом режиме `localhost:27017` из конфига будет указывать на MongoDB хоста.


## Nginx conf add 

В конфиге nginx добавляем работу с сервисом

    upstream saymon-tagman {
        server 127.0.0.1:3838;
    }
    location /nodeAF/ {
        rewrite ^/nodeAF/(.*) /$1 break;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Host $http_host;
        proxy_set_header X-NginX-Proxy true;

        proxy_pass http://saymon-tagman/;
        proxy_redirect off;
    }

## Очистка Docker-ресурсов только для tagmanager

Чтобы не занимать место и при этом не затрагивать другие контейнеры/образы, можно удалять только ресурсы этого сервиса.

Проверить текущие контейнеры и образы:

```bash
docker ps -a --filter name=tagmanager
docker images | rg tagmanager
```

Удалить контейнер и образ `tagmanager:latest`:

```bash
docker rm -f tagmanager 2>/dev/null || true
docker rmi tagmanager:latest 2>/dev/null || true
```

Удалить все теги репозитория `tagmanager` (если используются версии `v1`, `v2` и т.д.):

```bash
docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' | rg '^tagmanager:' | awk '{print $1}' | xargs -r docker rmi
```
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

- `HTTP_ADDR` - адрес HTTP сервера (по умолчанию `:8080`)
- `SAYMON_CONFIG_PATH` - путь к конфигу (по умолчанию `/etc/saymon/saymon-server.conf`)
- `TAGS_COLLECTION` - имя коллекции (по умолчанию `tags`)

## API

### Healthcheck

`GET /healthz`

- `200` и `{"status":"ok"}` если сервис жив и MongoDB доступна
- `503` и `{"status":"unhealthy","error":"mongodb unavailable"}` если MongoDB недоступна

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
  -p 8080:8080 \
  -e HTTP_ADDR=:8080 \
  -e SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf \
  -e TAGS_COLLECTION=tags \
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
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
  -p 8080:8080 \
  -e HTTP_ADDR=:8080 \
  -e SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf \
  -e TAGS_COLLECTION=tags \
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
```

Проверка после запуска:

```bash
curl http://127.0.0.1:8080/healthz
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
  -v /etc/saymon/saymon-server.conf:/etc/saymon/saymon-server.conf:ro \
  tagmanager:latest
```

Примечания:

- при `--network host` параметр `-p 8080:8080` не нужен;
- в этом режиме `localhost:27017` из конфига будет указывать на MongoDB хоста.

# Sing-Box Конфигуратор

Веб-приложение на Go и Vue 3 для управления sing-box: правилами маршрутизации, группами, DNS, outbound-ами,
inbound-ами и подписками.

## Архитектура

- **Точка правды — `app.json`.** Все настройки хранятся в нем. Конфиг sing-box не редактируется вручную:
  он рендерится из данных `app.json` и вшитого базового шаблона (`internal/render/base.json`).
- **Рабочий конфиг меняется только атомарно.** На странице «Конфиг» видны итоговый конфиг и diff с рабочим.
  При применении итоговый конфиг проверяется командой `sing-box check` внутри контейнера sing-box
  (через exec docker-controller), рабочий конфиг сохраняется в резервную копию и заменяется новым,
  sing-box перезапускается. Если sing-box не запустился, прежний конфиг восстанавливается. При случайном
  перезапуске sing-box всегда поднимается с проверенным конфигом.
- **Правила групп — remote rule-set-ы.** sing-box забирает их у конфигуратора (`/api/ruleset/...`)
  каждые 30 секунд, поэтому добавление правил и URL-источников не требует перезапуска sing-box.
- **Интерфейс** (`view/`) — Vue 3 и vue-router: каждая страница загружается по требованию,
  CodeMirror подгружается только на страницах с редактором и diff.

### Что рендерится

| Раздел | Откуда |
|---|---|
| `inbounds` | встроенные `tun-in` (tun0, 198.18.0.1/30, auto_redirect) и `dns-in` (0.0.0.0:53), mixed-прокси со страницы Inbounds |
| `outbounds`, `endpoints` | outbound-ы, добавленные вручную, серверы подписок Happ и Amnezia, urltest-ы со страницы Outbounds → URLTest, встроенные `direct`, `block` и selector-ы групп `select-<группа>` |
| `route` | служебные правила (bypass, sniff, hijack-dns, resolve для mixed-прокси, private → direct), `reject` для группы block, правила групп; `final: direct` |
| `dns` | DNS-серверы, DNS-записи (`configurer-hosts`), правила групп с DNS-сервером, фильтр HTTPS-записей, пользовательские DNS-правила, общие параметры |
| `experimental` | cache_file и Clash API на `0.0.0.0:9090` с токеном и CORS-origin-ами |
| `log` | уровень логов из общих настроек |

Маршрутные правила по умолчанию:

```json
[
  {"action": "bypass", "rule_set": "configurer-bypass"},
  {"action": "sniff", "timeout": "500ms"},
  {"action": "hijack-dns", "port": 53, "protocol": "dns"},
  {"action": "resolve", "inbound": "mixed-proxy"},
  {"ip_is_private": true, "outbound": "direct"}
]
```

## Интерфейс

- **Обзор** — трафик, подключения, график скорости и память sing-box (Clash API).
- **Прокси** — прокси-группы Clash API: переключение узла в selector-ах на лету, поиск и замер задержки.
- **Правила**
  - **Группы.** Каждая группа создает rule-set-ы `configurer-<группа>` (домены) и `configurer-<группа>@ip` (IP/CIDR)
    и selector `select-<группа>` с outbound-ом по умолчанию. Если у группы задан DNS-сервер, домены группы
    резолвятся через него. Системные группы: **block** — соединения отклоняются, **bypass** — трафик идет мимо
    туннеля sing-box. Новая инсталляция создает группу **default** (outbound `auto`, DNS-сервер `cloudflare`).
    Удалить можно только пустую группу.
  - **Одиночные** — домены, суффиксы, IP и CIDR. Изменения применяются кнопкой «Применить правила».
  - **Источники URL** — списки правил, которые конфигуратор периодически загружает.
- **DNS**
  - **Серверы** — UDP, TCP, DNS over TLS, DNS over HTTPS, DNS over HTTP/3, DNS over QUIC, local, DHCP.
    Популярные публичные серверы (Cloudflare, Google, Quad9, AdGuard, Яндекс) подставляются в один клик, адрес
    можно вставить ссылкой целиком (`https://dns.google/dns-query`). Поля без места в форме задаются JSON-ом
    «Дополнительные параметры».
  - **Записи** — домен и IP-адреса, которыми отвечает DNS sing-box.
  - **Настройки** — сервер по умолчанию (final), IP-версия, резолвер адресов outbound-ов, таймаут и кэш.
  - **Расширенные** — JSON: пользовательские DNS-правила (идут после системных) и дополнительные поля секции
    `dns` (например, `reverse_mapping`). Поля, которые задает конфигуратор (`servers`, `rules`, `final` и др.),
    в дополнительных полях запрещены.
- **Outbounds**
  - **Серверы** — share-ссылки (vless, hysteria2, trojan, wireguard) или JSON. Показываются и outbound-ы подписок.
  - **URLTest** — urltest-ы, состав которых собирается при каждом рендере: outbound-ы источников (все, добавленные
    вручную, подписка Happ или Amnezia целиком либо отдельный профиль), подходящие под regexp-фильтр по тегу,
    плюс явно добавленные, минус исключенные вручную или regexp-ом. Новая инсталляция создает urltest `auto`
    из всех outbound-ов; его можно изменить или удалить. urltest без outbound-ов в конфиг не попадает, группы
    с ним получают block. Удалить urltest, выбранный группой или указанный detour-ом DNS-сервера, нельзя.
- **Inbounds** — mixed-прокси (HTTP и SOCKS5) с пользователями.
- **Подписки** — Happ и Amnezia. Серверы подписок сразу попадают в итоговый конфиг. Точка в меню
  предупреждает, что подписка заканчивается: желтая — меньше недели или 75% трафика, красная — меньше
  3 дней, истекла или 90% трафика.
- **Конфиг** — итоговый конфиг, diff с рабочим и применение. Бейдж в меню горит, если итоговый конфиг
  отличается от рабочего.
- **Система**
  - **Настройки** — уровень логов, токен Clash API, CORS, плановая перезагрузка и перезапуск sing-box.
  - **Обновление** — доступные версии, список изменений и журнал последнего обновления.

### Плановая перезагрузка

Конфигуратор сам перезапускает sing-box по расписанию через docker-controller (раньше для этого был
отдельный контейнер cron-scheduler). Расписание задается в формате cron (минута, час, день месяца, месяц,
день недели; поддерживаются `*`, списки, диапазоны, шаги и `@daily`/`@hourly`/`@weekly`) и часовом поясе
IANA. По умолчанию — ежедневно в 06:00 UTC. Перезагрузка не выполняется одновременно с применением конфига.

### Мимо туннеля (группа bypass)

Трафик правил группы bypass sing-box не перехватывает: IP и CIDR попадают в `route_exclude_address_set`
tun-inbound-а и отсекаются в nftables, домены матчатся правилом `{"action": "bypass"}` в pre-match по
`dns.reverse_mapping`, поэтому клиенты должны резолвить имена через DNS sing-box. Работает на Linux
с `auto_redirect` (sing-box 1.13+).

### DNS-правила групп

Для группы с DNS-сервером в начале `dns.rules` создается правило `{"rule_set": "configurer-<группа>", "server": ...}`,
а первым — фильтр HTTPS-записей (без ECH-ключей клиенты отправляют настоящий SNI, и sniff видит домен).
Порядок: DNS-записи, фильтр HTTPS, правила групп, пользовательские правила. Пользовательские правила
не должны использовать legacy address filter (`ip_cidr`/`ip_is_private` без `match_response`).

Пока URL-источники группы не загружены после старта, эндпоинты rule-set-ов отвечают `503`: sing-box
оставляет закэшированный набор, а не получает неполный.

## Установка

Пример деплоя — каталог `deploy/`:

```
deploy/
  docker-compose.yaml
  sing-box-configurer.json
  docker-controller.json
  data/                 # app.json и резервные копии конфига sing-box (data/backups)
  sing-box/config.json  # рабочий конфиг sing-box (стартовый — рендер новой инсталляции)
```

Каталоги `data` и `sing-box` монтируются целиком: так конфигуратор заменяет файлы атомарно
(через временный файл и rename). При монтировании отдельных файлов запись выполняется на месте.

1. Сгенерируйте ключ docker-controller: `openssl rand -hex 32` и пропишите его в `docker-controller.json`
   (`api_key`) и `sing-box-configurer.json` (`docker_controller_api_key`).
2. Укажите в `sing-box-configurer.json` адрес Clash API sing-box (`clash_api_base_url`), доступный из
   контейнера конфигуратора (sing-box работает в сети хоста, например `http://192.168.1.10:9090`).
3. `docker compose up -d`, затем откройте `http://<хост>:8080`, настройте группы и DNS и примените конфиг.

### Переход с v0.5.x

Обновление через UI работает и со старыми монтированиями файлов. Чтобы запись конфигов стала атомарной,
один раз перенесите файлы в каталоги:

```bash
mkdir -p data sing-box
mv app.json data/app.json
mv sing-box.json sing-box/config.json
```

В `docker-compose.yaml` замените тома и команду sing-box как в `deploy/docker-compose.yaml`
(`./data:/app/data`, `./sing-box:/etc/sing-box`, `-c /etc/sing-box/config.json`), в
`sing-box-configurer.json` задайте `"app_data_path": "/app/data/app.json"` и `"sing_box_config_path":
"/etc/sing-box/config.json"`, затем `docker compose up -d`.

## Конфигурация

`config.json` конфигуратора:

```json
{
  "app_data_path": "/app/data/app.json",
  "listen_addr": ":8080",
  "docker_controller_url": "http://docker-controller:8081",
  "docker_controller_api_key": "",
  "source_lists_proxy_url": "",
  "sing_box_config_path": "/etc/sing-box/config.json",
  "clash_api_base_url": "http://127.0.0.1:9090",
  "clash_api_secret": "",
  "rule_set_base_url": "",
  "backup_dir": ""
}
```

- **app_data_path** — файл данных приложения (точка правды).
- **listen_addr** — адрес веб-интерфейса и API.
- **docker_controller_url**, **docker_controller_api_key** — docker-controller: перезапуск sing-box,
  `sing-box check` перед применением конфига и обновления.
- **source_lists_proxy_url** — прокси для загрузки URL-источников (необязательно).
- **sing_box_config_path** — рабочий конфиг sing-box.
- **clash_api_base_url** — адрес Clash API sing-box. Токен конфигуратор берет из рабочего конфига,
  **clash_api_secret** нужен, только если в рабочем конфиге токена нет.
- **rule_set_base_url** — адрес, по которому sing-box забирает rule-set-ы. По умолчанию
  `http://127.0.0.1:<порт listen_addr>` (sing-box работает в сети хоста).
- **backup_dir** — каталог резервных копий конфига sing-box (хранятся 10 последних). По умолчанию
  `backups` рядом с `app_data_path`.

## Разработка

```bash
cd view && npm ci && npm run build   # сборка интерфейса в view/dist (встраивается в бинарник)
go build -o sing-box-configurer ./cmd
cd view && npm run dev               # интерфейс с горячей перезагрузкой, API проксируется на :8080
```

Без сборки интерфейса бинарник собирается, но вместо интерфейса отдает заглушку.

## Обновления

Версия приложения равна тегу docker-образа (`docker.io/lanfix/sing-box-configurer:vX.Y.Z`).
Обновление запускается на странице «Система → Обновление» и выполняется атомарно: либо новая версия
запускается и проходит проверку, либо всё возвращается в исходное состояние.

### Как это работает

1. Конфигуратор через docker-controller запускает одноразовый контейнер `sing-box-configurer-updater`
   из образа **целевой** версии (бинарник `/app/updater`). Логика обновления всегда соответствует
   версии, на которую выполняется обновление.
2. Updater работает в сети compose-проекта и управляет контейнерами **только через API docker-controller**
   (доступа к `docker.sock` у него нет, смонтирована лишь папка деплоя):
   - скачивает образ новой версии;
   - при необходимости сначала обновляет docker-controller до версии, которую требует новый релиз
     (контроллер обновляет себя сам — см. ниже);
   - сохраняет бэкап смонтированных в конфигуратор файлов и каталогов, а также compose-файла в `.updates/<id>/backup/`;
   - через контроллер заменяет контейнер конфигуратора: старый останавливается и переименовывается
     (не удаляется), новый создается с теми же томами, портами, сетями и лейблами;
   - ждет `/api/health` новой версии: сервер отвечает только после успешных миграций данных;
   - прописывает новые теги образов в `docker-compose.yaml`;
   - удаляет старые контейнеры, хранит последние 5 папок `.updates`.
3. При любой ошибке updater удаляет новые контейнеры, восстанавливает файлы из бэкапа и запускает
   старые контейнеры. Причина и последние строки логов новой версии показываются в UI.
4. Каждый шаг записывается в журнал `.updates/<id>/journal.json`. Если updater упадет, docker
   перезапустит его, и он откатит незавершенное обновление.

**Обновление docker-controller.** Контроллер не может пересоздать себя через собственный API, поэтому
по запросу `POST /api/self-update` он запускает job из нового образа контроллера (`docker-controller self-update`).
Job заменяет контейнер контроллера, ждет ответа новой версии и при ошибке сам возвращает прежний контейнер.
Контроллер не хранит состояния, а его новые версии совместимы со старыми версиями конфигуратора, поэтому при
откате конфигуратора контроллер остается обновленным.

sing-box во время обновления не перезапускается и продолжает работать; недоступен только веб-интерфейс.

### Миграции данных

Версия схемы данных хранится в `app.json` (`schema_version`). Миграции описаны в
`internal/migrations/registry.go`, выполняются при старте до загрузки данных и только вперед
(откат — восстановление бэкапа). Новая миграция добавляется в конец списка со следующим номером;
старые миграции не удаляются и не меняются. Если данные новее, чем знает версия приложения,
сервис не запускается — это защищает от запуска старой версии на мигрированных данных.

### Релиз

```bash
./release.sh v1.2.3          # сборка образа (buildah или docker)
./release.sh v1.2.3 --push   # сборка и публикация
```

Скрипт проверяет формат semver и отсутствие тега в Docker Hub, передает версию в бинарники
и лейблы образа. Изменения релиза берутся из секции `## v1.2.3` файла `CHANGELOG.md`.
Если релиз требует новой версии docker-controller, поднимите `ControllerVersion` в
`internal/updater/updater.go` — updater обновит контроллер первым.

## API

Все изменения сохраняются в `app.json` и сразу попадают в итоговый конфиг; рабочий конфиг sing-box
меняется только через `POST /api/config/apply`. Ошибки возвращаются как `{"error": "..."}`.

| Метод | Путь | Описание |
|---|---|---|
| GET | `/api/health` | Версия приложения и схемы данных (для updater) |
| GET | `/api/ruleset/domain?group=`, `/api/ruleset/ip?group=`, `/api/ruleset/group?group=` | Rule-set группы: домены, IP или все значения (ETag, 304, 503 до загрузки источников) |
| GET | `/api/ruleset/bypass` | Rule-set группы bypass |
| GET/POST | `/api/rules`, `/api/rules/add`, `/api/rules/add-bulk`, `/api/rules/edit`, `/api/rules/delete`, `/api/apply` | Правила |
| GET/POST | `/api/groups`, `/api/groups/add`, `/api/groups/edit`, `/api/groups/delete` | Группы (системные — с `"system": true`) |
| GET/POST | `/api/url-sources`, `.../add`, `.../edit`, `.../delete`, `.../refresh`, `.../apply`, `.../validate`, `/api/url-sources/rules?id=` | URL-источники |
| GET/POST | `/api/dns`, `/api/dns/servers/add`, `.../edit`, `.../delete`, `/api/dns/settings`, `/api/dns/advanced` | DNS-серверы, настройки, пользовательские правила и дополнительные поля |
| GET/POST | `/api/dns-records`, `.../add`, `.../edit`, `.../delete` | DNS-записи |
| GET/POST | `/api/outbounds`, `.../add` (share-ссылка), `.../add-json`, `.../edit`, `.../delete` | Outbound-ы |
| GET/POST | `/api/urltests`, `.../add`, `.../edit`, `.../delete`, `.../preview` | urltest-ы (preview подбирает состав без сохранения) |
| GET/POST | `/api/inbounds`, `/api/inbounds/mixed/add`, `.../edit`, `.../delete` | Mixed-прокси |
| GET/POST | `/api/settings`, `/api/settings/regenerate-secret` | Уровень логов, токен и CORS Clash API |
| GET/POST | `/api/settings/restart` | Плановая перезагрузка: `{"enabled", "schedule", "timezone"}`, в ответе GET — также `next_run`, `last_run`, `last_error` |
| GET | `/api/config` | `{"rendered", "actual", "changed", "warnings", "actual_error"}` |
| GET | `/api/config/status` | `{"changed": true}` — итоговый конфиг отличается от рабочего |
| POST | `/api/config/apply` | Проверить, применить и перезапустить sing-box (409 — нет изменений, 422 — check не прошел) |
| POST | `/api/control/reload` | Перезапустить sing-box с рабочим конфигом |
| GET/POST | `/api/happ/profiles`, `.../add`, `.../refresh`, `.../delete` | Подписки Happ |
| GET/POST | `/api/amnezia/profiles`, `.../add`, `.../refresh`, `.../country`, `.../delete` | Конфигурации Amnezia |
| GET/POST | `/api/clash/overview`, `/api/clash/proxies`, `/api/clash/proxies/select`, `/api/clash/proxies/delay`, `/api/clash/group/delay` | Clash API |
| GET/POST | `/api/update/check`, `/api/update/start`, `/api/update/status` | Обновления |

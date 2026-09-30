# Sing-Box Конфигуратор

Веб-приложение на Go и Vue 3 для управления sing-box: правилами маршрутизации, группами, DNS, outbound-ами,
inbound-ами и подписками.

## Установка

Конфигуратор ставится на Linux-сервер или роутер вместе с sing-box
([sing-box-lx](https://github.com/Leadaxe/sing-box-lx) — сборка с поддержкой AmneziaWG). Есть два варианта:

- **Docker** — sing-box и конфигуратор в контейнерах docker compose;
- **systemd** — без Docker: оба работают службами systemd.

Возможности одинаковые, включая обновление конфигуратора из интерфейса. После установки откройте
`http://<адрес-сервера>:8080`, задайте логин и пароль в «Система → Настройки → Доступ к панели», настройте
группы, DNS и серверы и примените конфиг на странице «Конфиг».

**Требования**

- Linux на amd64, arm64 или armv7, доступ root.
- Свободный порт 53: sing-box принимает DNS-запросы на `0.0.0.0:53`. В Ubuntu и Debian его обычно занимает
  systemd-resolved — отключите его DNS-заглушку:

  ```bash
  sudo mkdir -p /etc/systemd/resolved.conf.d && printf '[Resolve]\nDNSStubListener=no\n' | sudo tee /etc/systemd/resolved.conf.d/no-stub.conf && sudo ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf && sudo systemctl restart systemd-resolved
  ```

- Свободный порт 8080 (панель) и 9090 (Clash API sing-box).

### Docker

Нужен Docker с плагином compose (`docker compose version`). Скопируйте блок целиком — он создаст
`/opt/sing-box-configurer/docker-compose.yaml` и запустит контейнеры:

```bash
sudo mkdir -p /opt/sing-box-configurer && cd /opt/sing-box-configurer && sudo tee docker-compose.yaml > /dev/null <<'EOF'
services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:latest
    container_name: sing-box-configurer
    restart: always
    ports:
      - 8080:8080
    # Clash API sing-box (сеть хоста) доступен конфигуратору по адресу host.docker.internal.
    extra_hosts:
      - host.docker.internal:host-gateway
    volumes:
      # app.json (все настройки) и резервные копии конфига sing-box.
      - ./data:/app/data
      # Рабочий конфиг sing-box: конфигуратор атомарно заменяет его при применении.
      - ./sing-box:/etc/sing-box
      # Управление контейнером sing-box и обновление конфигуратора.
      - /var/run/docker.sock:/var/run/docker.sock

  sing-box:
    image: docker.io/lanfix/sing-box-lx:v1.14.1-lx.8
    container_name: sing-box
    restart: always
    network_mode: host
    command: -D /var/lib/sing-box -c /etc/sing-box/config.json run
    # По этим лейблам конфигуратор находит контейнер sing-box.
    labels:
      - app=sing-box
      - managed=true
    depends_on:
      - sing-box-configurer
    volumes:
      - ./sing-box:/etc/sing-box:ro
      - sing-box:/var/lib/sing-box
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun

volumes:
  sing-box:
EOF
sudo docker compose up -d
```

При первом запуске конфигуратор записывает стартовый конфиг sing-box, и sing-box запускается с ним.
Каталоги `data` и `sing-box` монтируются целиком: так файлы заменяются атомарно (временный файл и rename).

Удаление: `cd /opt/sing-box-configurer && sudo docker compose down -v && cd / && sudo rm -rf /opt/sing-box-configurer`.

### systemd (без Docker)

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install.sh | sudo bash
```

Скрипт [`install.sh`](install.sh) скачивает sing-box-lx и последний релиз конфигуратора с GitHub (архивы
сверяются по `SHA256SUMS`), создает службы `sing-box` и `sing-box-configurer` и запускает их. Можно сразу
закрыть панель паролем:

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install.sh | sudo ADMIN_USER=admin ADMIN_PASSWORD='надежный-пароль' bash
```

Другие параметры (переменные окружения): `VERSION` — версия конфигуратора, `SING_BOX_VERSION` — версия
sing-box-lx, `SKIP_SING_BOX=1` — не ставить sing-box (он уже есть в `/usr/local/bin/sing-box`), `LISTEN_ADDR` —
адрес панели. Повторный запуск обновляет бинарники и юниты, данные не трогает.

| Путь | Что там |
|---|---|
| `/usr/local/bin/sing-box-configurer`, `/usr/local/bin/sing-box` | бинарники |
| `/etc/sing-box-configurer/config.json` | конфиг конфигуратора |
| `/var/lib/sing-box-configurer/` | `app.json`, резервные копии конфига sing-box, журналы обновлений |
| `/etc/sing-box/config.json` | рабочий конфиг sing-box |
| `/etc/systemd/system/sing-box.service`, `sing-box-configurer.service` | службы |

Логи: `journalctl -u sing-box-configurer -f` и `journalctl -u sing-box -f`.

Удаление:

```bash
sudo systemctl disable --now sing-box sing-box-configurer && sudo rm -f /etc/systemd/system/sing-box.service /etc/systemd/system/sing-box-configurer.service /usr/local/bin/sing-box /usr/local/bin/sing-box-configurer && sudo rm -rf /etc/sing-box-configurer /var/lib/sing-box-configurer /etc/sing-box /var/lib/sing-box && sudo systemctl daemon-reload
```

## Доступ к панели

Пока вход не включен, панелью и sing-box может управлять любой, у кого есть сетевой доступ к порту 8080.
Логин и пароль задаются в «Система → Настройки → Доступ к панели»; чтобы изменить или выключить вход,
нужен текущий пароль. Пароль хранится в `app.json` хэшем PBKDF2-SHA256, сессия — cookie на 30 дней, подписанная
ключом из `app.json` (смена логина или пароля завершает все прежние сессии). После 5 неверных попыток за
10 минут вход с этого адреса блокируется на 10 минут.

Без входа доступны только страница входа, `/api/health` (проверка при обновлении) и `/api/ruleset/...`
(rule-set-ы, которые забирает sing-box). Панель работает по HTTP — для доступа из интернета поставьте перед ней
reverse proxy с HTTPS.

Забыли пароль — выключите вход на сервере и перезапустите сервис:

```bash
# Docker
sudo docker exec sing-box-configurer /app/sing-box-configurer auth reset && sudo docker restart sing-box-configurer
# systemd
sudo sing-box-configurer -config /etc/sing-box-configurer/config.json auth reset && sudo systemctl restart sing-box-configurer
```

Задать логин и пароль из консоли: `auth set -username admin` (пароль читается со стандартного ввода или
передается флагом `-password`).

## Архитектура

- **Точка правды — `app.json`.** Все настройки хранятся в нем. Конфиг sing-box не редактируется вручную:
  он рендерится из данных `app.json` и вшитого базового шаблона (`internal/render/base.json`).
- **Рабочий конфиг меняется только атомарно.** На странице «Конфиг» видны итоговый конфиг и diff с рабочим.
  При применении итоговый конфиг проверяется командой `sing-box check`, рабочий конфиг сохраняется
  в резервную копию и заменяется новым, sing-box перезапускается. Если sing-box не запустился, прежний конфиг
  восстанавливается. При случайном перезапуске sing-box всегда поднимается с проверенным конфигом.
- **Платформа установки** (`internal/platform`) — интерфейсы управления sing-box (check, перезапуск,
  состояние, логи) и обновления конфигуратора. Реализации:
  - `docker` — Docker API через смонтированный `docker.sock` (`internal/dockerapi`): `sing-box check` выполняется
    через exec в контейнере sing-box (лейблы `app=sing-box`, `managed=true`), перезапуск — рестартом контейнера;
  - `systemd` — `sing-box check` бинарником sing-box, `systemctl restart`, логи из journald.

  Новый способ установки — еще одна реализация `platform.SingBox`, `platform.Updates` и `updater.Target`.
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

- **Обзор** — трафик, подключения, график скорости и память sing-box (Clash API), а под ними карта трафика:
  inbound-ы → правила маршрутизации по порядку → группы (selector-ы) → urltest-ы → outbound-ы. Карта строится
  из рабочего конфига, ее можно двигать и масштабировать, узлы — перетаскивать. Толстые связи — активный выбор,
  остальные варианты видны при выборе узла или с включенным «Все связи»; подписки с тремя и больше серверами
  свернуты в один узел. В боковой панели — сведения о группе, переключение узла selector-а, замер задержки
  urltest-а и соединения через выбранный элемент.
  - **Живой режим** привязывает соединения Clash API к связям карты: толщина и подпись — скорость, в панели —
    самые активные хосты.
  - **Поиск пути** по домену или IP (можно вставить URL) проходит правила рабочего конфига и показывает, какое
    из них сработает, какое правило группы совпало, какой DNS-сервер резолвит домен и в какой outbound уйдет
    соединение. Домен резолвится DNS-записями конфигуратора или системным резолвером, соединение считается
    TCP на 443 порт; правила с другими условиями отмечаются в примечаниях.
  - **DNS** — слой с DNS-правилами и серверами: у каждого сервера видно, через какой outbound уходят запросы.
    Сервер без detour sing-box подключает напрямую, мимо правил маршрутизации, — такая связь с direct
    нарисована пунктиром. Поиск пути показывает и цепочку, по которой уйдет DNS-запрос домена.
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
  - **Настройки** — доступ к панели (логин и пароль), уровень логов, токен Clash API, CORS, плановая
    перезагрузка и перезапуск sing-box.
  - **Обновление** — доступные версии, список изменений и журнал последнего обновления.

### Плановая перезагрузка

Конфигуратор сам перезапускает sing-box по расписанию (`docker restart` или `systemctl restart`). Расписание
задается в формате cron (минута, час, день месяца, месяц, день недели; поддерживаются `*`, списки,
диапазоны, шаги и `@daily`/`@hourly`/`@weekly`) и часовом поясе IANA. По умолчанию — ежедневно в 06:00 UTC.
Перезагрузка не выполняется одновременно с применением конфига.

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

## Конфигурация

Конфиг сервиса передается флагом `-config` (по умолчанию `config.json` в рабочем каталоге) и необязателен:
без него и для незаданных полей действуют значения по умолчанию платформы. Пример для systemd:

```json
{
  "platform": "systemd",
  "app_data_path": "/var/lib/sing-box-configurer/app.json",
  "listen_addr": ":8080",
  "source_lists_proxy_url": "",
  "sing_box_config_path": "/etc/sing-box/config.json",
  "clash_api_base_url": "http://127.0.0.1:9090",
  "clash_api_secret": "",
  "rule_set_base_url": "",
  "backup_dir": "",
  "systemd": {
    "sing_box_unit": "sing-box",
    "sing_box_binary": "/usr/local/bin/sing-box",
    "configurer_unit": "sing-box-configurer",
    "release_repository": "lanfix/sing-box-configurer"
  }
}
```

- **platform** — `docker` или `systemd`. По умолчанию определяется сам: `docker`, если сервис запущен
  в контейнере.
- **app_data_path** — файл данных приложения (точка правды). По умолчанию `/app/data/app.json` в docker
  и `/var/lib/sing-box-configurer/app.json` в systemd.
- **listen_addr** — адрес веб-интерфейса и API (`:8080`).
- **source_lists_proxy_url** — прокси для загрузки URL-источников (необязательно).
- **sing_box_config_path** — рабочий конфиг sing-box (`/etc/sing-box/config.json`).
- **clash_api_base_url** — адрес Clash API sing-box: `http://host.docker.internal:9090` в docker (sing-box
  работает в сети хоста), `http://127.0.0.1:9090` в systemd. Токен конфигуратор берет из рабочего конфига,
  **clash_api_secret** нужен, только если в рабочем конфиге токена нет.
- **rule_set_base_url** — адрес, по которому sing-box забирает rule-set-ы. По умолчанию
  `http://127.0.0.1:<порт listen_addr>`.
- **backup_dir** — каталог резервных копий конфига sing-box (хранятся 10 последних). По умолчанию
  `backups` рядом с `app_data_path`.
- **systemd** — службы sing-box и конфигуратора, бинарник sing-box для `sing-box check` и репозиторий
  GitHub, из релизов которого загружаются обновления.

## Обновления

Версия приложения равна тегу релиза. Обновление запускается на странице «Система → Обновление» и выполняется
атомарно: либо новая версия запускается и проходит проверку, либо всё возвращается в исходное состояние.
sing-box во время обновления не перезапускается и продолжает работать; недоступен только веб-интерфейс.

1. Конфигуратор запускает updater **целевой** версии, поэтому логика обновления всегда соответствует версии,
   на которую выполняется обновление:
   - docker — одноразовый контейнер `sing-box-configurer-updater` из образа новой версии (бинарник
     `/app/updater`) с доступом к `docker.sock` и папке деплоя (working_dir проекта compose);
   - systemd — архив релиза с GitHub (проверяется по `SHA256SUMS`), updater из него запускается
     transient-службой `sing-box-configurer-updater` (`systemd-run`) и переживает перезапуск конфигуратора.
2. Updater сохраняет бэкап данных в `.updates/<id>/backup/` (docker — смонтированные в конфигуратор файлы
   и compose-файл, systemd — `app.json`, конфиг сервиса и рабочий конфиг sing-box), затем заменяет версию:
   - docker — старый контейнер останавливается и переименовывается (не удаляется), новый создается с теми же
     томами, портами, сетями и лейблами; после проверки новый тег прописывается в `docker-compose.yaml`;
   - systemd — прежний бинарник сохраняется, новый атомарно встает на его место, служба перезапускается.
3. Updater ждет `/api/health` новой версии: сервер отвечает только после успешных миграций данных.
   При любой ошибке новая версия убирается, файлы восстанавливаются из бэкапа, прежняя версия запускается.
   Причина и последние строки логов новой версии показываются в UI.
4. Каждый шаг записывается в журнал `.updates/<id>/journal.json`. Если updater упадет, его перезапустят,
   и он откатит незавершенное обновление. Хранятся последние 5 папок `.updates`.

Общий ход обновления — `internal/updater`, шаги платформ — `updater.Target` в `internal/platform/docker`
и `internal/platform/systemd`.

### Миграции данных

Версия схемы данных хранится в `app.json` (`schema_version`). Миграции описаны в
`internal/migrations/registry.go`, выполняются при старте до загрузки данных и только вперед
(откат — восстановление бэкапа). Новая инсталляция сразу получает последнюю версию схемы, а данные по
умолчанию (группы, DNS-серверы, urltest `auto`, настройки) создают менеджеры при первом запуске.

Прежние миграции удалены: список начинается с версии схемы 6 (v0.9.0), новая миграция добавляется в конец
со следующим номером. Данные старше версии 6 и новее, чем знает версия приложения, сервис не запускает —
это защищает от запуска на неподходящих данных.

## Разработка

```bash
cd view && npm ci && npm run build   # сборка интерфейса в view/dist (встраивается в бинарник)
go build -o sing-box-configurer ./cmd
cd view && npm run dev               # интерфейс с горячей перезагрузкой, API проксируется на :8080
docker build -t sing-box-configurer .  # образ из исходников
```

Без сборки интерфейса бинарник собирается, но вместо интерфейса отдает заглушку.

### Релиз

Релиз собирает GitHub Actions ([`.github/workflows/release.yml`](.github/workflows/release.yml)) по тегу:

```bash
git tag v1.2.3 && git push origin v1.2.3
```

1. Интерфейс и бинарники `sing-box-configurer` и `updater` собираются один раз
   ([`scripts/build.sh`](scripts/build.sh)) для linux/amd64, linux/arm64 и linux/arm/v7.
2. Docker-образ `docker.io/lanfix/sing-box-configurer:v1.2.3` (и `latest` для релизов без суффикса)
   собирается из **этих же** бинарников: стадия `binaries` Dockerfile подменяется через
   `--build-context binaries=dist`.
3. Архивы `sing-box-configurer_v1.2.3_linux_<arch>.tar.gz` и `SHA256SUMS` ([`scripts/package.sh`](scripts/package.sh))
   публикуются в GitHub Release.

Изменения релиза берутся из секции `## v1.2.3` файла `CHANGELOG.md` ([`scripts/changelog.sh`](scripts/changelog.sh))
и попадают в текст GitHub Release и лейбл образа `io.lanfix.changelog`. Нужны секреты репозитория
`DOCKERHUB_USERNAME` и `DOCKERHUB_TOKEN`. Проверки (vet, тесты, сборка) на каждый push —
[`.github/workflows/ci.yml`](.github/workflows/ci.yml).

## API

Все изменения сохраняются в `app.json` и сразу попадают в итоговый конфиг; рабочий конфиг sing-box
меняется только через `POST /api/config/apply`. Ошибки возвращаются как `{"error": "..."}`. Если вход включен,
методы без сессии отвечают `401` (кроме помеченных как публичные).

| Метод | Путь | Описание |
|---|---|---|
| GET | `/api/health` | Версия приложения, платформа и версия схемы данных (для updater, публичный) |
| GET | `/api/auth/status` | `{"enabled", "authenticated", "username"}` (публичный) |
| POST | `/api/auth/login`, `/api/auth/logout` | Вход (`{"username", "password"}`, 429 — много попыток) и выход (публичные) |
| GET/POST | `/api/auth/settings` | Вход в панель: `{"enabled", "username", "password", "current_password"}` |
| GET | `/api/ruleset/domain?group=`, `/api/ruleset/ip?group=`, `/api/ruleset/group?group=` | Rule-set группы: домены, IP или все значения (ETag, 304, 503 до загрузки источников; публичный) |
| GET | `/api/ruleset/bypass` | Rule-set группы bypass (публичный) |
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
| GET | `/api/topology` | Карта трафика рабочего конфига: `{"nodes", "edges", "warnings"}` |
| GET | `/api/topology/connections` | Соединения Clash API, привязанные к карте (inbound, строка маршрутизатора, цепочка outbound-ов) |
| GET | `/api/topology/trace?query=<домен или IP>&inbound=<тег>` | Путь соединения: сработавшее правило, совпавшие правила групп, DNS-сервер, цепочка outbound-ов |
| GET/POST | `/api/update/check`, `/api/update/start`, `/api/update/status` | Обновления (в ответе check — также `platform`) |

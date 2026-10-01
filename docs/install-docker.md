# Установка через Docker

sing-box и конфигуратор работают контейнерами одного проекта docker compose. Конфигуратор управляет
контейнером sing-box через Docker API и обновляется из интерфейса.

## Установка одной командой

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-docker.sh | sudo bash
```

Сразу закрыть панель логином и паролем:

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-docker.sh | sudo ADMIN_USER=admin ADMIN_PASSWORD='надежный-пароль' bash
```

После установки откройте `http://<адрес-сервера>:8080` — адрес скрипт выводит в конце.

Скрипт [`install-docker.sh`](../install-docker.sh):

1. Ставит Docker официальным скриптом [get.docker.com](https://get.docker.com), если его нет.
2. Создает `/opt/sing-box-configurer/docker-compose.yaml` с последним релизом конфигуратора и sing-box-lx.
3. Загружает образы и запускает конфигуратор. При первом запуске он записывает стартовый конфиг sing-box.
4. Если порт 53 занят DNS-заглушкой systemd-resolved, отключает ее (см. [Порт 53](#порт-53)).
5. Запускает sing-box и проверяет, что он работает.

Повторный запуск не меняет `docker-compose.yaml` и данные: загружает образы и пересоздает только изменившиеся
контейнеры.

### Параметры

Задаются переменными окружения перед `bash`, как `ADMIN_USER` выше.

| Переменная | По умолчанию | Что задает |
|---|---|---|
| `ADMIN_USER`, `ADMIN_PASSWORD` | — | Логин и пароль панели. Без них панель открыта, пока вход не включат в интерфейсе |
| `PORT` | `8080` | Порт панели на хосте |
| `INSTALL_DIR` | `/opt/sing-box-configurer` | Папка проекта compose |
| `VERSION` | последний релиз | Версия конфигуратора |
| `SING_BOX_VERSION` | `v1.14.1-lx.8` | Версия образа [sing-box-lx](https://github.com/Leadaxe/sing-box-lx) |
| `INSTALL_DOCKER=0` | — | Не ставить Docker, а завершиться с ошибкой, если его нет |
| `KEEP_RESOLVED=1` | — | Не отключать DNS-заглушку systemd-resolved |

`PORT`, `VERSION` и `SING_BOX_VERSION` действуют только при первой установке, когда `docker-compose.yaml`
еще нет.

## Требования

- Linux на amd64, arm64 или armv7, доступ root, устройство `/dev/net/tun`.
- Docker с плагином compose — скрипт поставит его сам.
- Свободные порты 53 (DNS sing-box), 8080 (панель), 9090 (Clash API sing-box) и 9091 (служебный inbound
  для загрузки URL-источников). sing-box работает в сети хоста.

## Порт 53

sing-box принимает DNS-запросы на `0.0.0.0:53`. В Ubuntu и Debian порт обычно занимает DNS-заглушка
systemd-resolved (`127.0.0.53`) — скрипт отключает ее файлом
`/etc/systemd/resolved.conf.d/sing-box-configurer.conf` и направляет `/etc/resolv.conf` на DNS провайдера.
Свой `/etc/resolv.conf` (не ссылку на заглушку) скрипт не трогает.

Если вы направили DNS самого сервера на sing-box (`nameserver 127.0.0.1` в `/etc/resolv.conf`), добавьте
запасной сервер: иначе, пока sing-box остановлен, сервер не резолвит имена и не скачает образы и обновления.

```bash
sudo rm -f /etc/resolv.conf && printf 'nameserver 127.0.0.1\nnameserver 1.1.1.1\noptions timeout:1 attempts:1\n' | sudo tee /etc/resolv.conf
```

## docker-compose.yaml

Скрипт создает такой файл (теги образов — версии на момент установки):

```yaml
services:
  sing-box-configurer:
    image: docker.io/lanfix/sing-box-configurer:v0.11.0
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
```

Каталоги `data` и `sing-box` монтируются целиком: так файлы заменяются атомарно (временный файл и rename).
Теги образов записаны явно: при обновлении из интерфейса updater прописывает здесь новый тег конфигуратора.

| Путь | Что там |
|---|---|
| `/opt/sing-box-configurer/docker-compose.yaml` | проект compose |
| `/opt/sing-box-configurer/data/` | `app.json` (все настройки), резервные копии конфига sing-box, кэш URL-источников |
| `/opt/sing-box-configurer/sing-box/config.json` | рабочий конфиг sing-box |
| `/opt/sing-box-configurer/.updates/` | журналы и бэкапы обновлений |
| том `sing-box-configurer_sing-box` | кэш sing-box (`cache_file`) |

Логи: `sudo docker logs -f sing-box-configurer` и `sudo docker logs -f sing-box`.

## Обновление

Конфигуратор обновляется из интерфейса: «Система → Обновление». Если меняете `docker-compose.yaml` вручную
(например, версию sing-box), не останавливайте весь проект (`docker compose down`): пока sing-box выключен, хост
может остаться без DNS и VPN и не скачает образы. Загрузите образы заранее и пересоздайте только изменившиеся
контейнеры:

```bash
cd /opt/sing-box-configurer && sudo docker compose pull && sudo docker compose up -d --remove-orphans
```

## Восстановление доступа

```bash
# Забыли пароль: выключить вход
sudo docker exec sing-box-configurer /app/sing-box-configurer auth reset && sudo docker restart sing-box-configurer
# Панель не открывается по домену: выключить проверку адреса
sudo docker exec sing-box-configurer /app/sing-box-configurer security reset && sudo docker restart sing-box-configurer
```

## Удаление

```bash
cd /opt/sing-box-configurer && sudo docker compose down -v && cd / && sudo rm -rf /opt/sing-box-configurer
```

Вернуть DNS-заглушку systemd-resolved, если ее отключил скрипт:

```bash
sudo rm -f /etc/systemd/resolved.conf.d/sing-box-configurer.conf && sudo ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf && sudo systemctl restart systemd-resolved
```

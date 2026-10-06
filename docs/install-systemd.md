# Установка через systemd (без Docker)

sing-box и конфигуратор работают службами systemd. Конфигуратор управляет sing-box через `systemctl`
и обновляется из интерфейса: архивы релизов загружаются с GitHub.

## Установка одной командой

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-systemd.sh | sudo bash
```

Сразу закрыть панель логином и паролем:

```bash
curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-systemd.sh | sudo ADMIN_USER=admin ADMIN_PASSWORD='надежный-пароль' bash
```

После установки откройте `http://<адрес-сервера>:8080` — адрес скрипт выводит в конце.

Скрипт [`install-systemd.sh`](../install-systemd.sh):

1. Скачивает [sing-box-lx](https://github.com/lanfix/sing-box-lx) и последний релиз конфигуратора с GitHub.
   Архивы сверяются по `SHA256SUMS` релиза.
2. Создает конфиг сервиса и службы `sing-box` и `sing-box-configurer`.
3. Запускает конфигуратор. При первом запуске он записывает стартовый конфиг sing-box.
4. Если порт 53 занят DNS-заглушкой systemd-resolved, отключает ее (см. [Порт 53](#порт-53)).
5. Запускает sing-box и проверяет, что он работает.

Повторный запуск обновляет бинарники и юниты, данные и конфиги не трогает.

### Параметры

Задаются переменными окружения перед `bash`, как `ADMIN_USER` выше.

| Переменная | По умолчанию | Что задает |
|---|---|---|
| `ADMIN_USER`, `ADMIN_PASSWORD` | — | Логин и пароль панели. Без них панель открыта, пока вход не включат в интерфейсе |
| `LISTEN_ADDR` | `:8080` | Адрес панели (только при первой установке) |
| `VERSION` | последний релиз | Версия конфигуратора |
| `SING_BOX_VERSION` | `v1.14.2-lx.11-mac.1` | Версия sing-box-lx |
| `SING_BOX_REPO` | `lanfix/sing-box-lx` | Репозиторий GitHub с релизами sing-box-lx (например, `Leadaxe/sing-box-lx` для его версий) |
| `SKIP_SING_BOX=1` | — | Не ставить sing-box: он уже есть в `/usr/local/bin/sing-box` |
| `KEEP_RESOLVED=1` | — | Не отключать DNS-заглушку systemd-resolved |

## Требования

- Linux с systemd на amd64, arm64 или armv7, доступ root, устройство `/dev/net/tun`.
- `curl`, `tar`, `sha256sum`.
- Свободные порты 53 (DNS sing-box), 8080 (панель), 9090 (Clash API sing-box) и 9091 (служебный inbound
  для загрузки URL-источников, слушает `127.0.0.1`).

## Порт 53

sing-box принимает DNS-запросы на `0.0.0.0:53`. В Ubuntu и Debian порт обычно занимает DNS-заглушка
systemd-resolved (`127.0.0.53`) — скрипт отключает ее файлом
`/etc/systemd/resolved.conf.d/sing-box-configurer.conf` и направляет `/etc/resolv.conf` на DNS провайдера.
Свой `/etc/resolv.conf` (не ссылку на заглушку) скрипт не трогает.

Если вы направили DNS самого сервера на sing-box (`nameserver 127.0.0.1` в `/etc/resolv.conf`), добавьте
запасной сервер: иначе, пока sing-box остановлен, сервер не резолвит имена и не скачает обновления.

```bash
sudo rm -f /etc/resolv.conf && printf 'nameserver 127.0.0.1\nnameserver 1.1.1.1\noptions timeout:1 attempts:1\n' | sudo tee /etc/resolv.conf
```

## Файлы

| Путь | Что там |
|---|---|
| `/usr/local/bin/sing-box-configurer`, `/usr/local/bin/sing-box` | бинарники |
| `/etc/sing-box-configurer/config.json` | конфиг сервиса (см. «Конфигурация» в [README](../README.md#конфигурация)) |
| `/var/lib/sing-box-configurer/` | `app.json` (все настройки), резервные копии конфига sing-box, кэш URL-источников, журналы обновлений (`.updates`) |
| `/etc/sing-box/config.json` | рабочий конфиг sing-box |
| `/var/lib/sing-box/` | кэш sing-box (`cache_file`) |
| `/etc/systemd/system/sing-box.service`, `sing-box-configurer.service` | службы |

Логи: `journalctl -u sing-box-configurer -f` и `journalctl -u sing-box -f`.

## Обновление

Конфигуратор обновляется из интерфейса: «Система → Обновление». Если установлен sing-box-lx старше версии, которая
нужна новому конфигуратору, updater загружает архив [lanfix/sing-box-lx](https://github.com/lanfix/sing-box-lx/releases)
(проверка по `SHA256SUMS`) и после проверки конфигуратора заменяет `/usr/local/bin/sing-box` и перезапускает
службу sing-box (если новый sing-box не запустился, возвращается прежний). Официальный sing-box и более новые
версии не трогаются.

Версию sing-box можно сменить и повторным запуском скрипта с `SING_BOX_VERSION`: он заменит бинарник
и перезапустит службы.

## Восстановление доступа

```bash
# Забыли пароль: выключить вход
sudo sing-box-configurer -config /etc/sing-box-configurer/config.json auth reset && sudo systemctl restart sing-box-configurer
# Панель не открывается по домену: выключить проверку адреса
sudo sing-box-configurer -config /etc/sing-box-configurer/config.json security reset && sudo systemctl restart sing-box-configurer
```

## Удаление

```bash
sudo systemctl disable --now sing-box sing-box-configurer && sudo rm -f /etc/systemd/system/sing-box.service /etc/systemd/system/sing-box-configurer.service /usr/local/bin/sing-box /usr/local/bin/sing-box-configurer && sudo rm -rf /etc/sing-box-configurer /var/lib/sing-box-configurer /etc/sing-box /var/lib/sing-box && sudo systemctl daemon-reload
```

Вернуть DNS-заглушку systemd-resolved, если ее отключил скрипт:

```bash
sudo rm -f /etc/systemd/resolved.conf.d/sing-box-configurer.conf && sudo ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf && sudo systemctl restart systemd-resolved
```

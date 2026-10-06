#!/usr/bin/env bash

# Установка sing-box-configurer и sing-box контейнерами docker compose.
#
#   curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-docker.sh | sudo bash
#
# Создает папку проекта с docker-compose.yaml и запускает контейнеры. Повторный запуск не меняет
# docker-compose.yaml и данные: загружает образы и пересоздает изменившиеся контейнеры. Подробности —
# docs/install-docker.md.
#
# Переменные окружения:
#   INSTALL_DIR       — папка проекта compose (по умолчанию /opt/sing-box-configurer)
#   VERSION           — версия конфигуратора для новой установки (по умолчанию последний релиз)
#   SING_BOX_VERSION  — версия образа sing-box-lx для новой установки (по умолчанию v1.14.2-lx.11-mac.1)
#   PORT              — порт панели на хосте для новой установки (по умолчанию 8080)
#   ADMIN_USER, ADMIN_PASSWORD — сразу закрыть панель логином и паролем
#   INSTALL_DOCKER=0  — не устанавливать Docker, если его нет (по умолчанию ставится скриптом get.docker.com)
#   KEEP_RESOLVED=1   — не отключать DNS-заглушку systemd-resolved, которая занимает порт 53

set -euo pipefail

REPO="lanfix/sing-box-configurer"
IMAGE="docker.io/lanfix/sing-box-configurer"
SING_BOX_IMAGE="docker.io/lanfix/sing-box-lx"
SING_BOX_VERSION="${SING_BOX_VERSION:-v1.14.2-lx.11-mac.1}"
INSTALL_DIR="${INSTALL_DIR:-/opt/sing-box-configurer}"
PORT="${PORT:-8080}"

log() {
    echo "==> $*"
}

warn() {
    echo "warning: $*" >&2
}

die() {
    echo "error: $*" >&2
    exit 1
}

[[ "$(id -u)" -eq 0 ]] || die "run as root: curl -fsSL ... | sudo bash"
command -v curl >/dev/null 2>&1 || die "curl is required"
[[ "$PORT" =~ ^[0-9]+$ ]] || die "PORT must be a number"

case "$(uname -m)" in
    x86_64 | amd64 | aarch64 | arm64 | armv7l | armv7) ;;
    *) die "unsupported architecture $(uname -m)" ;;
esac

[[ -c /dev/net/tun ]] || warn "/dev/net/tun is missing: sing-box needs it for the tun inbound"

# free_dns_port освобождает порт 53 для DNS sing-box (0.0.0.0:53): отключает DNS-заглушку systemd-resolved
# на 127.0.0.53. Сервер после этого резолвит имена через DNS провайдера из /run/systemd/resolve/resolv.conf.
free_dns_port() {
    [[ "${KEEP_RESOLVED:-}" == "1" ]] && return 0
    command -v systemctl >/dev/null 2>&1 || return 0
    systemctl is-active --quiet systemd-resolved 2>/dev/null || return 0

    if command -v ss >/dev/null 2>&1 && ! ss -lnu 2>/dev/null | grep -q '127\.0\.0\.53%\?[a-z0-9]*:53 '; then
        return 0
    fi

    log "Disabling the systemd-resolved DNS stub listener: sing-box listens for DNS on 0.0.0.0:53 (KEEP_RESOLVED=1 to skip)"

    mkdir -p /etc/systemd/resolved.conf.d
    printf '[Resolve]\nDNSStubListener=no\n' > /etc/systemd/resolved.conf.d/sing-box-configurer.conf

    # Свой /etc/resolv.conf не трогаем, заменяем только ссылку на заглушку.
    if [[ "$(readlink -f /etc/resolv.conf)" == */stub-resolv.conf ]]; then
        ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf
    fi

    systemctl restart systemd-resolved
}

# wait_for проверяет команду раз в секунду, пока она не выполнится или не пройдет seconds секунд.
wait_for() {
    local seconds="$1"
    shift

    for _ in $(seq 1 "$seconds"); do
        "$@" && return 0
        sleep 1
    done

    return 1
}

# container_running проверяет, что контейнер name запущен.
container_running() {
    [[ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" == "true" ]]
}

if ! command -v docker >/dev/null 2>&1; then
    [[ "${INSTALL_DOCKER:-1}" == "1" ]] || die "Docker is required: https://docs.docker.com/engine/install/"

    log "Installing Docker (https://get.docker.com, INSTALL_DOCKER=0 to skip)"
    curl -fsSL https://get.docker.com | sh
fi

if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now docker >/dev/null 2>&1 || true
fi

docker info >/dev/null 2>&1 || die "Docker is not running"
docker compose version >/dev/null 2>&1 || die "Docker Compose plugin is required: https://docs.docker.com/compose/install/linux/"

mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/sing-box"
chmod 700 "$INSTALL_DIR/data"
cd "$INSTALL_DIR"

if [[ -f docker-compose.yaml ]]; then
    log "Using existing $INSTALL_DIR/docker-compose.yaml"
else
    if [[ -z "${VERSION:-}" ]]; then
        VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)" || true
        [[ -n "$VERSION" ]] || die "cannot find the latest release of $REPO, set VERSION"
    fi

    log "Writing $INSTALL_DIR/docker-compose.yaml (sing-box-configurer $VERSION, sing-box $SING_BOX_VERSION)"

    # Теги образов записываются явно: при обновлении из интерфейса updater заменяет тег конфигуратора здесь же.
    cat > docker-compose.yaml <<EOF
services:
  sing-box-configurer:
    image: $IMAGE:$VERSION
    container_name: sing-box-configurer
    restart: always
    ports:
      - $PORT:8080
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
    image: $SING_BOX_IMAGE:$SING_BOX_VERSION
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
fi

# Образы загружаются заранее: пока sing-box остановлен, хост может остаться без DNS и VPN.
log "Pulling images"
docker compose pull

log "Starting sing-box-configurer"
docker compose up -d --remove-orphans sing-box-configurer

port="$(docker compose port sing-box-configurer 8080 2>/dev/null | sed 's/.*://')"
port="${port:-$PORT}"

wait_for 60 curl -fs -o /dev/null "http://127.0.0.1:${port}/api/health" ||
    die "sing-box-configurer did not start, see: docker logs sing-box-configurer"

# При первом запуске конфигуратор записывает стартовый конфиг sing-box.
wait_for 30 test -f "$INSTALL_DIR/sing-box/config.json" ||
    die "sing-box config was not created, see: docker logs sing-box-configurer"

if [[ -n "${ADMIN_USER:-}" && -n "${ADMIN_PASSWORD:-}" ]]; then
    log "Enabling panel login for $ADMIN_USER"
    # Пароль передается через stdin: аргументы командной строки видны всем пользователям в списке процессов.
    printf '%s\n' "$ADMIN_PASSWORD" | docker exec -i sing-box-configurer /app/sing-box-configurer auth set -username "$ADMIN_USER" >/dev/null
    docker restart sing-box-configurer >/dev/null

    wait_for 60 curl -fs -o /dev/null "http://127.0.0.1:${port}/api/health" ||
        die "sing-box-configurer did not restart, see: docker logs sing-box-configurer"
fi

free_dns_port

log "Starting sing-box"
docker compose up -d --remove-orphans

# sing-box может упасть не сразу (например, если порт занят): проверяем, что он работает несколько секунд.
sleep 5

# Адрес для ссылки на панель: hostname -I есть не везде (например, в Alpine), тогда — источник маршрута по умолчанию.
address="$(hostname -I 2>/dev/null | awk '{print $1}')" || true

if [[ -z "$address" ]]; then
    address="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}')" || true
fi

echo
echo "Done. Panel: http://${address:-<server-ip>}:${port}"
echo "Project: $INSTALL_DIR (docker compose)"

if ! container_running sing-box; then
    warn "sing-box is not running, see: docker logs sing-box"
    warn "port 53 may be busy: the rendered config listens for DNS on 0.0.0.0:53"
fi

if ! curl -fsS "http://127.0.0.1:${port}/api/auth/status" 2>/dev/null | grep -q '"enabled":true'; then
    echo "The panel is open without authentication: set a login in System → Security → Panel access."
fi

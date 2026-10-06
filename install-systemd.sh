#!/usr/bin/env bash

# Установка sing-box-configurer и sing-box службами systemd, без Docker.
#
#   curl -fsSL https://raw.githubusercontent.com/lanfix/sing-box-configurer/master/install-systemd.sh | sudo bash
#
# Повторный запуск обновляет бинарники и юниты, данные и конфиги не трогает. Подробности — docs/install-systemd.md.
#
# Переменные окружения:
#   VERSION           — версия конфигуратора (по умолчанию последний релиз)
#   SING_BOX_VERSION  — версия sing-box-lx (по умолчанию v1.14.2-lx.11-mac.1)
#   SING_BOX_REPO     — репозиторий GitHub с релизами sing-box-lx (по умолчанию lanfix/sing-box-lx)
#   SKIP_SING_BOX=1   — не устанавливать sing-box (он уже есть в /usr/local/bin/sing-box)
#   LISTEN_ADDR       — адрес панели для новой установки (по умолчанию :8080)
#   ADMIN_USER, ADMIN_PASSWORD — сразу закрыть панель логином и паролем
#   KEEP_RESOLVED=1   — не отключать DNS-заглушку systemd-resolved, которая занимает порт 53

set -euo pipefail

REPO="lanfix/sing-box-configurer"
SING_BOX_REPO="${SING_BOX_REPO:-lanfix/sing-box-lx}"
SING_BOX_VERSION="${SING_BOX_VERSION:-v1.14.2-lx.11-mac.1}"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"

BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/sing-box-configurer"
DATA_DIR="/var/lib/sing-box-configurer"
SING_BOX_CONFIG_DIR="/etc/sing-box"
SING_BOX_DATA_DIR="/var/lib/sing-box"
UNIT_DIR="/etc/systemd/system"

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
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

for cmd in curl tar sha256sum; do
    command -v "$cmd" >/dev/null 2>&1 || die "$cmd is required"
done

case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    armv7l | armv7) ARCH=armv7 ;;
    *) die "unsupported architecture $(uname -m)" ;;
esac

[[ -c /dev/net/tun ]] || warn "/dev/net/tun is missing: sing-box needs it for the tun inbound"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# download_verified загружает архив релиза и проверяет его по SHA256SUMS релиза.
download_verified() {
    local base="$1" name="$2"

    curl -fsSL -o "$TMP/SHA256SUMS" "$base/SHA256SUMS" || die "cannot download $base/SHA256SUMS"
    curl -fsSL -o "$TMP/$name" "$base/$name" || die "cannot download $base/$name"

    (cd "$TMP" && grep -E "[[:space:]]\*?${name}\$" SHA256SUMS | sha256sum -c - >/dev/null) || die "checksum mismatch for $name"
}

# extract_binary распаковывает из архива файл name и устанавливает его в BIN_DIR/target.
extract_binary() {
    local archive="$1" name="$2" target="$3"
    local dir="$TMP/extract-$name"

    mkdir -p "$dir"
    tar -xzf "$TMP/$archive" -C "$dir"

    local file
    file="$(find "$dir" -type f -name "$name" | head -n 1)"
    [[ -n "$file" ]] || die "$archive has no $name"

    install -m 755 "$file" "$BIN_DIR/$target.new"
    mv -f "$BIN_DIR/$target.new" "$BIN_DIR/$target"
}

# free_dns_port освобождает порт 53 для DNS sing-box (0.0.0.0:53): отключает DNS-заглушку systemd-resolved
# на 127.0.0.53. Сервер после этого резолвит имена через DNS провайдера из /run/systemd/resolve/resolv.conf.
free_dns_port() {
    [[ "${KEEP_RESOLVED:-}" == "1" ]] && return 0
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

if [[ -z "${VERSION:-}" ]]; then
    VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
    [[ -n "$VERSION" ]] || die "cannot find the latest release of $REPO"
fi

if [[ "${SKIP_SING_BOX:-}" != "1" ]]; then
    log "Installing sing-box $SING_BOX_VERSION ($ARCH)"

    archive="sing-box-${SING_BOX_VERSION#v}-linux-${ARCH}.tar.gz"
    download_verified "https://github.com/$SING_BOX_REPO/releases/download/$SING_BOX_VERSION" "$archive"
    extract_binary "$archive" sing-box sing-box
fi

[[ -x "$BIN_DIR/sing-box" ]] || die "$BIN_DIR/sing-box is not installed"

log "Installing sing-box-configurer $VERSION ($ARCH)"

archive="sing-box-configurer_${VERSION}_linux_${ARCH}.tar.gz"
download_verified "https://github.com/$REPO/releases/download/$VERSION" "$archive"
extract_binary "$archive" sing-box-configurer sing-box-configurer

mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$SING_BOX_CONFIG_DIR" "$SING_BOX_DATA_DIR"
chmod 700 "$DATA_DIR"

if [[ ! -f "$CONFIG_DIR/config.json" ]]; then
    log "Writing $CONFIG_DIR/config.json"

    cat > "$CONFIG_DIR/config.json" <<EOF
{
  "platform": "systemd",
  "app_data_path": "$DATA_DIR/app.json",
  "listen_addr": "$LISTEN_ADDR",
  "sing_box_config_path": "$SING_BOX_CONFIG_DIR/config.json",
  "systemd": {
    "sing_box_unit": "sing-box",
    "sing_box_binary": "$BIN_DIR/sing-box",
    "configurer_unit": "sing-box-configurer"
  }
}
EOF
fi

log "Writing systemd units"

cat > "$UNIT_DIR/sing-box-configurer.service" <<EOF
[Unit]
Description=Sing-Box Configurer
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN_DIR/sing-box-configurer -config $CONFIG_DIR/config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

cat > "$UNIT_DIR/sing-box.service" <<EOF
[Unit]
Description=sing-box (managed by sing-box-configurer)
After=network-online.target sing-box-configurer.service
Wants=network-online.target

[Service]
ExecStart=$BIN_DIR/sing-box -D $SING_BOX_DATA_DIR -c $SING_BOX_CONFIG_DIR/config.json run
Restart=always
RestartSec=5
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
EOF

if [[ -n "${ADMIN_USER:-}" && -n "${ADMIN_PASSWORD:-}" ]]; then
    log "Enabling panel login for $ADMIN_USER"
    # Пароль передается через stdin: аргументы командной строки видны всем пользователям в списке процессов.
    printf '%s\n' "$ADMIN_PASSWORD" | "$BIN_DIR/sing-box-configurer" -config "$CONFIG_DIR/config.json" auth set -username "$ADMIN_USER" >/dev/null
fi

systemctl daemon-reload

log "Starting sing-box-configurer"
systemctl enable sing-box-configurer >/dev/null 2>&1
systemctl restart sing-box-configurer

port="${LISTEN_ADDR##*:}"

wait_for 30 curl -fs -o /dev/null "http://127.0.0.1:${port}/api/health" ||
    die "sing-box-configurer did not start, see: journalctl -u sing-box-configurer -n 50"

# При первом запуске конфигуратор записывает стартовый конфиг sing-box.
wait_for 30 test -f "$SING_BOX_CONFIG_DIR/config.json" ||
    die "sing-box config was not created, see: journalctl -u sing-box-configurer -n 50"

free_dns_port

log "Starting sing-box"
systemctl enable sing-box >/dev/null 2>&1
systemctl restart sing-box

# sing-box может упасть не сразу (например, если порт занят): проверяем, что он работает несколько секунд.
sleep 5

# Адрес для ссылки на панель: hostname -I есть не везде (например, в Alpine), тогда — источник маршрута по умолчанию.
address="$(hostname -I 2>/dev/null | awk '{print $1}')" || true

if [[ -z "$address" ]]; then
    address="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}')" || true
fi

echo
echo "Done. Panel: http://${address:-<server-ip>}:${port}"

if ! systemctl is-active --quiet sing-box; then
    warn "sing-box is not running, see: journalctl -u sing-box -n 50"
    warn "port 53 may be busy: the rendered config listens for DNS on 0.0.0.0:53"
fi

if ! curl -fsS "http://127.0.0.1:${port}/api/auth/status" 2>/dev/null | grep -q '"enabled":true'; then
    echo "The panel is open without authentication: set a login in System → Security → Panel access."
fi

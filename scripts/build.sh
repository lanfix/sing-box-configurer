#!/bin/sh

# Сборка бинарников sing-box-configurer и updater. Используется и при сборке docker-образа
# (Dockerfile), и в релизе (.github/workflows/release.yml): в образ и в архивы релиза попадают
# одни и те же бинарники.
#
# Использование:
#   scripts/build.sh VERSION [PLATFORM...]
#
# PLATFORM — linux/amd64, linux/arm64 или linux/arm/v7 (по умолчанию все три).
# Бинарники пишутся в $DIST/<os>_<arch><variant>/ (по умолчанию dist/linux_amd64 и т.д.).
# Интерфейс (view/dist) собирается заранее: npm ci && npm run build в каталоге view.

set -eu

MODULE=github.com/lanfix/sing-box-configurer
VERSION="${1:?Usage: $0 VERSION [PLATFORM...]}"
DIST="${DIST:-dist}"

shift

if [ "$#" -eq 0 ]; then
    set -- linux/amd64 linux/arm64 linux/arm/v7
fi

cd "$(dirname "$0")/.."

if [ ! -f view/dist/index.html ]; then
    echo "warning: view/dist is not built, the binary will serve a stub page" >&2
fi

for platform in "$@"; do
    os="${platform%%/*}"
    rest="${platform#*/}"
    arch="${rest%%/*}"
    variant=""

    if [ "$rest" != "$arch" ]; then
        variant="${rest#*/}"
    fi

    goarm=""

    if [ "$arch" = "arm" ]; then
        goarm="${variant#v}"
    fi

    out="$DIST/${os}_${arch}${variant}"
    mkdir -p "$out"

    for app in sing-box-configurer:./cmd updater:./cmd/updater; do
        name="${app%%:*}"

        echo "==> $VERSION $platform $name"

        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM="$goarm" go build -trimpath \
            -ldflags="-s -w -X $MODULE/internal/version.Version=$VERSION" \
            -o "$out/$name" "${app#*:}"
    done
done

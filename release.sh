#!/usr/bin/env bash
# Сборка (и опционально публикация) docker-образа релиза.
#
# Использование:
#   ./release.sh v1.2.3 [--push]
#
# Переменные окружения:
#   IMAGE    — репозиторий образа (по умолчанию docker.io/lanfix/sing-box-configurer)
#   BUILDER  — buildah или docker (по умолчанию buildah, если установлен)
#
# Версия образа = версия приложения. Изменения для релиза берутся из секции
# "## vX.Y.Z" файла CHANGELOG.md (если она есть) и попадают в лейбл io.lanfix.changelog.

set -euo pipefail

IMAGE="${IMAGE:-docker.io/lanfix/sing-box-configurer}"
SEMVER_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'

usage() {
    echo "Usage: $0 vX.Y.Z [--push]" >&2
    exit 2
}

die() {
    echo "error: $*" >&2
    exit 1
}

VERSION="${1:-}"
PUSH=false

[[ -n "$VERSION" ]] || usage
shift

for arg in "$@"; do
    case "$arg" in
        --push) PUSH=true ;;
        *) usage ;;
    esac
done

[[ "$VERSION" =~ $SEMVER_RE ]] || die "version '$VERSION' is not semver (expected vMAJOR.MINOR.PATCH[-PRERELEASE])"

if [[ -z "${BUILDER:-}" ]]; then
    if command -v buildah >/dev/null 2>&1; then
        BUILDER=buildah
    else
        BUILDER=docker
    fi
fi

command -v "$BUILDER" >/dev/null 2>&1 || die "builder '$BUILDER' not found"

cd "$(dirname "$0")"

# Проверяем, что такой тег ещё не опубликован (только для Docker Hub).
if [[ "$IMAGE" == docker.io/* ]]; then
    repo="${IMAGE#docker.io/}"
    status="$(curl -s -o /dev/null -w '%{http_code}' "https://hub.docker.com/v2/repositories/${repo}/tags/${VERSION}" || true)"

    [[ "$status" != "200" ]] || die "tag ${IMAGE}:${VERSION} already exists in registry"
fi

# Версия docker-controller, которую потребует updater этого релиза, должна быть опубликована:
# иначе обновление упадет на загрузке образа контроллера.
CONTROLLER_VERSION="$(sed -n 's/^[[:space:]]*ControllerVersion = "\(v[^"]*\)".*/\1/p' internal/updater/updater.go)"

[[ -n "$CONTROLLER_VERSION" ]] || die "cannot find ControllerVersion in internal/updater/updater.go"

status="$(curl -s -o /dev/null -w '%{http_code}' "https://hub.docker.com/v2/repositories/lanfix/docker-controller/tags/${CONTROLLER_VERSION}" || true)"

[[ "$status" == "200" ]] || die "docker-controller ${CONTROLLER_VERSION} (ControllerVersion) is not published in Docker Hub"

# Секция изменений из CHANGELOG.md: строки после "## vX.Y.Z" до следующего заголовка "## ".
CHANGELOG=""

if [[ -f CHANGELOG.md ]]; then
    CHANGELOG="$(awk -v ver="$VERSION" '
        /^## / { if (found) exit; found = ($2 == ver); next }
        found { print }
    ' CHANGELOG.md | sed '/^[[:space:]]*$/d')"
fi

echo "==> Building ${IMAGE}:${VERSION} with ${BUILDER}"

build_args=(--build-arg "VERSION=${VERSION}" --build-arg "CHANGELOG=${CHANGELOG}" -t "${IMAGE}:${VERSION}" .)

if [[ "$BUILDER" == buildah ]]; then
    buildah build --layers "${build_args[@]}"
else
    docker build "${build_args[@]}"
fi

if [[ "$PUSH" == true ]]; then
    echo "==> Pushing ${IMAGE}:${VERSION}"
    "$BUILDER" push "${IMAGE}:${VERSION}"
fi

echo "==> Done: ${IMAGE}:${VERSION}"

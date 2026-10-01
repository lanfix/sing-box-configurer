#!/bin/sh

# Упаковка бинарников из scripts/build.sh в архивы релиза и SHA256SUMS.
#
# Использование:
#   scripts/package.sh VERSION
#
# Для каждого каталога $DIST/<os>_<arch> создается $DIST/release/sing-box-configurer_VERSION_<os>_<arch>.tar.gz
# с бинарниками sing-box-configurer и updater. Имена архивов ожидает обновление systemd-инсталляций
# (internal/platform/systemd) и install-systemd.sh.

set -eu

VERSION="${1:?Usage: $0 VERSION}"
DIST="${DIST:-dist}"

cd "$(dirname "$0")/.."

release="$DIST/release"
rm -rf "$release"
mkdir -p "$release"

for dir in "$DIST"/*_*/; do
    target="$(basename "$dir")"
    name="sing-box-configurer_${VERSION}_${target}"
    staging="$release/$name"

    mkdir -p "$staging"
    cp "$dir/sing-box-configurer" "$dir/updater" "$staging/"
    chmod 755 "$staging/sing-box-configurer" "$staging/updater"

    tar -C "$release" -czf "$release/$name.tar.gz" "$name"
    rm -rf "$staging"

    echo "==> $release/$name.tar.gz"
done

(cd "$release" && sha256sum ./*.tar.gz | sed 's| \./| |' > SHA256SUMS)

cat "$release/SHA256SUMS"

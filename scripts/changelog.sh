#!/bin/sh

# Выводит изменения версии из CHANGELOG.md: строки после "## VERSION" до следующего заголовка "## ".
# Используется как текст релиза GitHub и лейбл io.lanfix.changelog docker-образа.
#
# Использование:
#   scripts/changelog.sh VERSION

set -eu

VERSION="${1:?Usage: $0 VERSION}"

cd "$(dirname "$0")/.."

awk -v ver="$VERSION" '
    /^## / { if (found) exit; found = ($2 == ver); next }
    found { print }
' CHANGELOG.md | sed '/^[[:space:]]*$/d'

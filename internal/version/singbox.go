package version

import (
	"strconv"
	"strings"
)

const (
	// SingBoxVersion — версия sing-box-lx, до которой updater обновляет sing-box вместе с конфигуратором.
	SingBoxVersion = "v1.14.2-lx.11-mac.1"

	// SingBoxRepository — образ sing-box-lx в Docker Hub (без docker.io/).
	SingBoxRepository = "lanfix/sing-box-lx"

	// SingBoxReleaseRepository — репозиторий GitHub с архивами sing-box-lx для установки через systemd.
	SingBoxReleaseRepository = "lanfix/sing-box-lx"
)

// CompareSingBox сравнивает версии sing-box-lx вида v1.14.2-lx.11 или 1.14.2-lx.11-mac.1: -1, 0 или 1.
// ok равен false, если одна из версий не в этом формате (например, официальный sing-box).
func CompareSingBox(a, b string) (int, bool) {
	left, ok := parseSingBox(a)
	if !ok {
		return 0, false
	}

	right, ok := parseSingBox(b)
	if !ok {
		return 0, false
	}

	for i := range left {
		switch {
		case left[i] < right[i]:
			return -1, true

		case left[i] > right[i]:
			return 1, true
		}
	}

	return 0, true
}

// parseSingBox разбирает версию в числа: major, minor, patch, номер lx и номер сборки поверх lx (0, если ее нет).
func parseSingBox(value string) ([5]int, bool) {
	var result [5]int

	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), "-")
	if len(parts) < 2 || len(parts) > 3 {
		return result, false
	}

	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return result, false
	}

	lx, ok := strings.CutPrefix(parts[1], "lx.")
	if !ok {
		return result, false
	}

	numbers := append(core, lx)

	// Сборка поверх релиза lx: <задача>.<номер>, например mac.1.
	if len(parts) == 3 {
		_, build, found := strings.Cut(parts[2], ".")
		if !found {
			return result, false
		}

		numbers = append(numbers, build)
	}

	for i, number := range numbers {
		parsed, err := strconv.Atoi(number)
		if err != nil || parsed < 0 {
			return result, false
		}

		result[i] = parsed
	}

	return result, true
}

// Package semver разбирает и сравнивает версии вида v1.2.3 и v1.2.3-rc.1.
package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// pattern совпадает с регулярным выражением в scripts/build.sh.
var pattern = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?$`)

// Version — разобранная версия.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

// Parse разбирает версию. Префикс "v" обязателен.
func Parse(value string) (Version, error) {
	match := pattern.FindStringSubmatch(value)
	if match == nil {
		return Version{}, fmt.Errorf("invalid semver %q", value)
	}

	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])

	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: match[4],
	}, nil
}

// IsValid проверяет, что строка является версией semver.
func IsValid(value string) bool {
	_, err := Parse(value)

	return err == nil
}

// String возвращает версию в каноничном виде.
func (v Version) String() string {
	result := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)

	if v.Prerelease != "" {
		result += "-" + v.Prerelease
	}

	return result
}

// Compare возвращает -1, 0 или 1, если v меньше, равна или больше other.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		if pair[0] != pair[1] {
			return compareInt(pair[0], pair[1])
		}
	}

	return comparePrerelease(v.Prerelease, other.Prerelease)
}

// Compare сравнивает две строки-версии. Невалидные версии считаются меньше любых валидных.
func Compare(a, b string) int {
	va, errA := Parse(a)
	vb, errB := Parse(b)

	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)

	case errA != nil:
		return -1

	case errB != nil:
		return 1
	}

	return va.Compare(vb)
}

// comparePrerelease сравнивает pre-release части по правилам semver 2.0.
// Версия без pre-release старше версии с pre-release.
func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0

	case a == "":
		return 1

	case b == "":
		return -1
	}

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		numA, errA := strconv.Atoi(partsA[i])
		numB, errB := strconv.Atoi(partsB[i])

		switch {
		case errA == nil && errB == nil:
			if numA != numB {
				return compareInt(numA, numB)
			}

		case errA == nil:
			return -1

		case errB == nil:
			return 1

		default:
			if c := strings.Compare(partsA[i], partsB[i]); c != 0 {
				return c
			}
		}
	}

	return compareInt(len(partsA), len(partsB))
}

// compareInt сравнивает два числа.
func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1

	case a > b:
		return 1
	}

	return 0
}

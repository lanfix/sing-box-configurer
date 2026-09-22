package semver

import (
	"testing"
)

// TestParse проверяет разбор валидных и невалидных версий.
func TestParse(t *testing.T) {
	valid := []string{"v0.0.1", "v1.2.3", "v10.20.30", "v1.0.0-rc.1", "v1.0.0-alpha-2.x"}
	invalid := []string{"1.2.3", "v1.2", "v01.2.3", "v1.2.3-", "v1.2.3+build", "latest", "dev", ""}

	for _, value := range valid {
		if !IsValid(value) {
			t.Errorf("%q must be valid", value)
		}
	}

	for _, value := range invalid {
		if IsValid(value) {
			t.Errorf("%q must be invalid", value)
		}
	}
}

// TestCompare проверяет порядок версий, включая pre-release.
func TestCompare(t *testing.T) {
	ordered := []string{
		"v0.0.9",
		"v0.0.10",
		"v0.1.0",
		"v1.0.0-alpha",
		"v1.0.0-alpha.1",
		"v1.0.0-alpha.beta",
		"v1.0.0-beta.2",
		"v1.0.0-beta.11",
		"v1.0.0-rc.1",
		"v1.0.0",
		"v1.10.0",
	}

	for i := 0; i < len(ordered)-1; i++ {
		if Compare(ordered[i], ordered[i+1]) != -1 {
			t.Errorf("%s must be less than %s", ordered[i], ordered[i+1])
		}

		if Compare(ordered[i+1], ordered[i]) != 1 {
			t.Errorf("%s must be greater than %s", ordered[i+1], ordered[i])
		}
	}

	if Compare("dev", "v0.0.1") != -1 {
		t.Error("invalid version must be less than valid one")
	}
}

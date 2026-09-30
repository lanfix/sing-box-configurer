package docker

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

// deployPaths сопоставляет пути папки деплоя на хосте с путями относительно нее.
type deployPaths struct {
	// hostDir — папка деплоя на хосте (working_dir проекта docker compose).
	hostDir string
}

// relative возвращает путь относительно папки деплоя для пути на хосте.
func (p deployPaths) relative(hostPath string) (string, bool) {
	hostDir := strings.TrimSuffix(path.Clean(hostPath), "/")
	base := strings.TrimSuffix(path.Clean(p.hostDir), "/")

	rel, ok := strings.CutPrefix(hostDir, base+"/")
	if !ok || rel == "" || strings.HasPrefix(rel, "../") {
		return "", false
	}

	return rel, true
}

// composeFiles возвращает пути compose-файлов проекта на хосте из лейблов контейнера.
func composeFiles(labels map[string]string) []string {
	result := make([]string, 0)

	for _, file := range strings.Split(labels[composeConfigFilesLabel], ",") {
		if file = strings.TrimSpace(file); file != "" {
			result = append(result, file)
		}
	}

	return result
}

// setComposeImageTag заменяет тег образа repository (например docker.io/lanfix/sing-box-configurer)
// в строках "image:" compose-файла. Возвращает true, если файл изменился.
func setComposeImageTag(file, repository, tag string) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return false, fmt.Errorf("cannot read compose file: %w", err)
	}

	// Короткая форма (lanfix/x) и полная (docker.io/lanfix/x) — один и тот же образ.
	short := strings.TrimPrefix(repository, "docker.io/")
	pattern := regexp.MustCompile(`(?m)^(\s*image:\s*["']?)((?:docker\.io/)?` + regexp.QuoteMeta(short) + `):[^\s"'@]+`)

	updated := pattern.ReplaceAll(data, []byte("${1}${2}:"+tag))
	if string(updated) == string(data) {
		return false, nil
	}

	if err = os.WriteFile(file, updated, 0644); err != nil {
		return false, fmt.Errorf("cannot write compose file: %w", err)
	}

	return true, nil
}

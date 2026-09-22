package updater

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// deployPaths сопоставляет пути папки деплоя на хосте и внутри контейнера updater.
type deployPaths struct {
	// hostDir — папка деплоя на хосте (working_dir проекта docker compose).
	hostDir string

	// localDir — та же папка, смонтированная в контейнер updater.
	localDir string
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

// local возвращает путь внутри контейнера updater для пути относительно папки деплоя.
func (p deployPaths) local(rel string) string {
	return filepath.Join(p.localDir, filepath.FromSlash(rel))
}

// backupFiles копирует файлы папки деплоя в dir/backup.
func backupFiles(paths deployPaths, dir string, files []string) ([]BackupFile, error) {
	result := make([]BackupFile, 0, len(files))

	for _, rel := range files {
		data, err := os.ReadFile(paths.local(rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, err)
		}

		target := filepath.Join(dir, "backup", filepath.FromSlash(rel))

		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("cannot create backup dir: %w", err)
		}

		if err = os.WriteFile(target, data, 0644); err != nil {
			return nil, fmt.Errorf("cannot write backup of %s: %w", rel, err)
		}

		result = append(result, BackupFile{
			Path: rel,
		})
	}

	return result, nil
}

// restoreFiles восстанавливает файлы из бэкапа. Файлы пишутся in-place: контейнеры монтируют
// отдельные файлы, и замена через rename подменила бы inode, который они не увидят.
func restoreFiles(paths deployPaths, dir string, files []BackupFile) error {
	var errs []string

	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, "backup", filepath.FromSlash(file.Path)))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Path, err))

			continue
		}

		if err = os.WriteFile(paths.local(file.Path), data, 0644); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Path, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cannot restore files: %s", strings.Join(errs, "; "))
	}

	return nil
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

// pruneUpdates удаляет старые папки обновлений, оставляя keep последних и папку current.
func pruneUpdates(updatesDir, current string, keep int) error {
	entries, err := os.ReadDir(updatesDir)
	if err != nil {
		return fmt.Errorf("cannot read updates dir: %w", err)
	}

	type dirInfo struct {
		name    string
		modTime int64
	}

	dirs := make([]dirInfo, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		dirs = append(dirs, dirInfo{
			name:    entry.Name(),
			modTime: info.ModTime().UnixNano(),
		})
	}

	// Новые первыми.
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime > dirs[j].modTime
	})

	for i := keep - 1; i < len(dirs); i++ {
		if i < 0 {
			continue
		}

		if err = os.RemoveAll(filepath.Join(updatesDir, dirs[i].name)); err != nil {
			return fmt.Errorf("cannot remove %s: %w", dirs[i].name, err)
		}
	}

	return nil
}

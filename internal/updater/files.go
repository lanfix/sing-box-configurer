package updater

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	// maxBackupFileSize — файлы крупнее в каталогах данных не бэкапятся (это не данные приложения).
	maxBackupFileSize = 16 << 20
)

// skipBackupDirs — каталоги внутри каталогов данных, которые не бэкапятся.
var skipBackupDirs = []string{UpdatesDirName, "backups"}

// localPath возвращает путь файла rel (относительно root) в файловой системе updater.
func localPath(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// expandFiles раскрывает каталоги списка paths (относительно root) в обычные файлы.
// Несуществующие пути пропускаются.
func expandFiles(root string, paths []string) []string {
	files := make([]string, 0, len(paths))

	for _, rel := range paths {
		stat, err := os.Stat(localPath(root, rel))
		if err != nil {
			continue
		}

		// Каталог данных (например, data с app.json) бэкапится файлами.
		if stat.IsDir() {
			files = appendUnique(files, dirFiles(root, rel)...)

			continue
		}

		if stat.Mode().IsRegular() {
			files = appendUnique(files, rel)
		}
	}

	return files
}

// dirFiles возвращает пути (относительно root) обычных файлов каталога rel.
func dirFiles(root, rel string) []string {
	files := make([]string, 0)
	dir := localPath(root, rel)

	_ = filepath.WalkDir(dir, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if entry.IsDir() {
			if current != dir && slices.Contains(skipBackupDirs, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxBackupFileSize {
			return nil
		}

		inner, err := filepath.Rel(dir, current)
		if err != nil {
			return nil
		}

		files = append(files, path.Join(rel, filepath.ToSlash(inner)))

		return nil
	})

	return files
}

// appendUnique добавляет в список значения, которых в нем еще нет.
func appendUnique(list []string, values ...string) []string {
	for _, value := range values {
		if !slices.Contains(list, value) {
			list = append(list, value)
		}
	}

	return list
}

// backupFiles копирует файлы (пути относительно root) в dir/backup.
func backupFiles(root, dir string, files []string) ([]BackupFile, error) {
	result := make([]BackupFile, 0, len(files))

	for _, rel := range files {
		data, err := os.ReadFile(localPath(root, rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, err)
		}

		target := filepath.Join(dir, "backup", filepath.FromSlash(rel))

		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("cannot create backup dir: %w", err)
		}

		if err = os.WriteFile(target, data, 0600); err != nil {
			return nil, fmt.Errorf("cannot write backup of %s: %w", rel, err)
		}

		result = append(result, BackupFile{
			Path: rel,
		})
	}

	return result, nil
}

// restoreFiles восстанавливает файлы из бэкапа. Файлы пишутся in-place: контейнеры могут монтировать
// отдельные файлы, и замена через rename подменила бы inode, который они не увидят.
func restoreFiles(root, dir string, files []BackupFile) error {
	var errs []string

	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, "backup", filepath.FromSlash(file.Path)))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Path, err))

			continue
		}

		target := localPath(root, file.Path)

		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Path, err))

			continue
		}

		if err = os.WriteFile(target, data, 0644); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file.Path, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cannot restore files: %s", strings.Join(errs, "; "))
	}

	return nil
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

// Package fsutil содержит операции с файлами, общие для хранилищ приложения.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic записывает data в файл path так, чтобы читатель видел либо старое, либо новое содержимое
// целиком: данные пишутся во временный файл рядом и переименовываются поверх path.
//
// Если файл смонтирован в контейнер отдельным bind mount-ом, rename поверх него невозможен (EBUSY),
// и файл перезаписывается на месте. Атомарность в этом случае не гарантируется, поэтому рекомендуется
// монтировать каталог, а не файл.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		// Каталог недоступен для записи — остается запись на месте.
		return writeInPlace(path, data, perm)
	}

	tmpPath := tmp.Name()

	if err = writeAndSync(tmp, data); err != nil {
		_ = os.Remove(tmpPath)

		return err
	}

	if err = os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("cannot chmod %s: %w", tmpPath, err)
	}

	if err = os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)

		return writeInPlace(path, data, perm)
	}

	syncDir(filepath.Dir(path))

	return nil
}

// writeAndSync записывает данные в открытый файл, сбрасывает их на диск и закрывает файл.
func writeAndSync(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return fmt.Errorf("cannot write %s: %w", file.Name(), err)
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()

		return fmt.Errorf("cannot sync %s: %w", file.Name(), err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", file.Name(), err)
	}

	return nil
}

// writeInPlace перезаписывает файл на месте, сохраняя inode (нужно для файлов под bind mount-ом).
func writeInPlace(path string, data []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", path, err)
	}

	return writeAndSync(file, data)
}

// syncDir сбрасывает на диск запись каталога после rename. Ошибки игнорируются: не все ФС это поддерживают.
func syncDir(dir string) {
	file, err := os.Open(dir)
	if err != nil {
		return
	}

	_ = file.Sync()
	_ = file.Close()
}

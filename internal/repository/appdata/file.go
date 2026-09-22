package appdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrNotExist возвращается, если файл данных приложения ещё не создан.
var ErrNotExist = os.ErrNotExist

// File — общий JSON-файл данных приложения (app.json).
// Разные менеджеры хранят в нем свои поля верхнего уровня и не затирают чужие.
type File struct {
	path string
	mu   sync.Mutex
}

// NewFile создает доступ к файлу данных приложения по пути path.
func NewFile(path string) *File {
	return &File{
		path: path,
		mu:   sync.Mutex{},
	}
}

// Read читает файл и раскладывает его в v. Поля, которых нет в v, игнорируются.
// Если файла нет или он пустой, возвращается ErrNotExist.
func (f *File) Read(v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	raw, err := f.readLocked()
	if err != nil {
		return err
	}

	if err = json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("cannot parse app data: %w", err)
	}

	return nil
}

// Merge записывает поля верхнего уровня из v в файл, сохраняя остальные поля без изменений.
// Поля v с omitempty и пустым значением не попадут в файл, и там останется старое значение.
func (f *File) Merge(v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	patchRaw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cannot marshal app data: %w", err)
	}

	var patch map[string]json.RawMessage

	if err = json.Unmarshal(patchRaw, &patch); err != nil {
		return fmt.Errorf("app data must be a json object: %w", err)
	}

	current := map[string]json.RawMessage{}

	raw, err := f.readLocked()
	if err != nil && !errors.Is(err, ErrNotExist) {
		return err
	}

	if err == nil {
		if err = json.Unmarshal(raw, &current); err != nil {
			return fmt.Errorf("cannot parse app data: %w", err)
		}
	}

	for key, value := range patch {
		current[key] = value
	}

	// encoding/json сортирует ключи map, поэтому порядок полей в файле стабилен.
	result, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal app data: %w", err)
	}

	// Пишем in-place, а не через rename, чтобы не ломать bind mount файла в docker.
	if err = os.WriteFile(f.path, append(result, '\n'), 0644); err != nil {
		return fmt.Errorf("cannot write app data: %w", err)
	}

	return nil
}

// readLocked читает содержимое файла. Вызывается под блокировкой.
func (f *File) readLocked() ([]byte, error) {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotExist
		}

		return nil, fmt.Errorf("cannot read app data: %w", err)
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, ErrNotExist
	}

	return raw, nil
}

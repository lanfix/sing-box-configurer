package appdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/lanfix/sing-box-configurer/internal/fsutil"
)

var (
	// ErrNotExist возвращается, если файл данных приложения ещё не создан.
	ErrNotExist = os.ErrNotExist

	// ErrFrozen возвращается при записи после Replace: файл заменен целиком и ждет перезапуска приложения.
	ErrFrozen = errors.New("app data is replaced, restart is pending")
)

// File — общий JSON-файл данных приложения (app.json).
// Разные менеджеры хранят в нем свои поля верхнего уровня и не затирают чужие.
type File struct {
	path string
	mu   sync.Mutex

	// frozen — файл заменен через Replace: менеджеры держат в памяти прежние данные, и их запись
	// затерла бы новые разделы.
	frozen bool
}

// NewFile создает доступ к файлу данных приложения по пути path.
func NewFile(path string) *File {
	return &File{
		path:   path,
		mu:     sync.Mutex{},
		frozen: false,
	}
}

// Path возвращает путь к файлу данных.
func (f *File) Path() string {
	return f.path
}

// Replace полностью перезаписывает файл полями fields и запрещает дальнейшую запись до перезапуска
// приложения (ErrFrozen): данные менеджеров в памяти больше не соответствуют файлу.
func (f *File) Replace(fields map[string]json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.writeLocked(fields); err != nil {
		return err
	}

	f.frozen = true

	return nil
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

// ReadRaw возвращает поля верхнего уровня файла без разбора значений.
// Если файла нет или он пустой, возвращается ErrNotExist.
func (f *File) ReadRaw() (map[string]json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	raw, err := f.readLocked()
	if err != nil {
		return nil, err
	}

	var fields map[string]json.RawMessage

	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("cannot parse app data: %w", err)
	}

	return fields, nil
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

	return f.writeLocked(current)
}

// WriteRaw полностью перезаписывает файл полями fields. Поля, которых нет в fields, удаляются.
func (f *File) WriteRaw(fields map[string]json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.writeLocked(fields)
}

// writeLocked записывает поля в файл. Вызывается под блокировкой.
func (f *File) writeLocked(fields map[string]json.RawMessage) error {
	if f.frozen {
		return ErrFrozen
	}

	// encoding/json сортирует ключи map, поэтому порядок полей в файле стабилен.
	result, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal app data: %w", err)
	}

	// В файле хранятся хэш пароля, ключ подписи сессий и ключи прокси: читать его может только владелец.
	// Права файлов, созданных прежними версиями (0644), ужесточаются до записи: запись их сохраняет.
	if info, statErr := os.Stat(f.path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(f.path, info.Mode().Perm()&^0o077)
	}

	// При монтировании файла отдельным bind mount-ом запись выполняется на месте, иначе — атомарно.
	if err = fsutil.WriteFileAtomic(f.path, append(result, '\n'), 0600); err != nil {
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

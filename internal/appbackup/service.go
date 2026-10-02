// Package appbackup выгружает данные приложения (app.json) и загружает их из файла: данные проверяются,
// приводятся миграциями к актуальной схеме и заменяют текущие, после чего приложение перезапускается.
package appbackup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/fsutil"
	"github.com/lanfix/sing-box-configurer/internal/migrations"
	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

const (
	// backupPrefix — префикс копий app.json, которые сохраняются перед импортом.
	backupPrefix = "app-"

	// keepBackups — сколько копий app.json хранить.
	keepBackups = 5

	// Разделы app.json с входом в панель и защитой адреса панели.
	authKey     = "auth"
	settingsKey = "settings"
	securityKey = "security"
)

// knownSections — разделы app.json. В файле импорта должен быть хотя бы один из них.
var knownSections = []string{
	"auth", "settings", "groups", "rules", "url_sources", "dns", "dns_records", "outbounds", "urltests", "inbounds",
	"happ", "amnezia",
}

var (
	// ErrInvalidData — файл не является данными приложения.
	ErrInvalidData = errors.New("invalid app data")

	// ErrUnsupportedSchema — схема данных файла не поддерживается этой версией приложения.
	ErrUnsupportedSchema = errors.New("unsupported app data schema")

	// ErrImported — данные уже заменены, приложение ждет перезапуска.
	ErrImported = errors.New("app data is already imported, restart is pending")
)

// Validator проверяет, что данные из файла path загружаются менеджерами приложения.
type Validator func(path string) error

// ImportOptions — параметры импорта.
type ImportOptions struct {
	// ReplaceAccess — взять из файла вход в панель (логин и пароль) и разрешенные адреса панели.
	// По умолчанию они остаются текущими, чтобы импорт не лишил доступа к панели.
	ReplaceAccess bool
}

// ImportResult — итог импорта.
type ImportResult struct {
	// FromVersion — версия схемы файла, ToVersion — версия после миграций.
	FromVersion int      `json:"from_version"`
	ToVersion   int      `json:"to_version"`
	Migrations  []string `json:"migrations"`

	// Backup — копия прежних данных.
	Backup string `json:"backup"`
}

// Service выгружает и загружает данные приложения.
type Service struct {
	appData   *appdata.File
	singBox   migrations.SingBoxConfigStore
	backupDir string
	validate  Validator

	// Импорты выполняются по одному, imported — данные уже заменены.
	mu       sync.Mutex
	imported bool
}

// NewService создает сервис. Копии прежних данных сохраняются в backupDir, singBox дает миграциям
// рабочий конфиг sing-box (изменения не записываются).
func NewService(appData *appdata.File, singBox migrations.SingBoxConfigStore, backupDir string, validate Validator) *Service {
	return &Service{
		appData:   appData,
		singBox:   singBox,
		backupDir: backupDir,
		validate:  validate,
		mu:        sync.Mutex{},
		imported:  false,
	}
}

// Export возвращает содержимое app.json с отступами.
func (s *Service) Export() ([]byte, error) {
	fields, err := s.appData.ReadRaw()
	if err != nil {
		return nil, fmt.Errorf("cannot read app data: %w", err)
	}

	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot marshal app data: %w", err)
	}

	return append(data, '\n'), nil
}

// Summary — что содержит файл импорта.
type Summary struct {
	// SchemaVersion — версия схемы файла, LatestVersion — версия схемы этой версии приложения.
	SchemaVersion int `json:"schema_version"`
	LatestVersion int `json:"latest_version"`

	// Counts — число элементов разделов: groups, rules, url_sources, outbounds, urltests, dns_servers,
	// dns_records, mixed, happ, amnezia.
	Counts map[string]int `json:"counts"`

	// HasAuth — в файле включен вход в панель, Username — логин входа.
	HasAuth  bool   `json:"has_auth"`
	Username string `json:"username,omitempty"`

	// AllowedHosts — разрешенные доменные имена панели из файла.
	AllowedHosts []string `json:"allowed_hosts"`
}

// Inspect проверяет файл импорта data и возвращает его содержимое. Данные не меняются.
func (s *Service) Inspect(data []byte) (*Summary, error) {
	fields, err := parse(data)
	if err != nil {
		return nil, err
	}

	version, err := migrations.SchemaVersion(fields)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidData, err)
	}

	summary := &Summary{
		SchemaVersion: version,
		LatestVersion: migrations.LatestVersion(),
		Counts:        map[string]int{},
		HasAuth:       false,
		Username:      "",
		AllowedHosts:  []string{},
	}

	for _, key := range []string{"groups", "rules", "url_sources", "outbounds", "urltests", "dns_records"} {
		summary.Counts[key] = countArray(fields[key])
	}

	summary.Counts["dns_servers"] = countNested(fields["dns"], "servers")
	summary.Counts["mixed"] = countNested(fields["inbounds"], "mixed")
	summary.Counts["happ"] = countNested(fields["happ"], "profiles")
	summary.Counts["amnezia"] = countNested(fields["amnezia"], "profiles")

	var access struct {
		Auth *struct {
			Enabled  bool   `json:"enabled"`
			Username string `json:"username"`
		} `json:"auth"`
		Settings *struct {
			Security *struct {
				AllowedHosts []string `json:"allowed_hosts"`
			} `json:"security"`
		} `json:"settings"`
	}

	_ = json.Unmarshal(data, &access)

	if access.Auth != nil && access.Auth.Enabled {
		summary.HasAuth = true
		summary.Username = access.Auth.Username
	}

	if access.Settings != nil && access.Settings.Security != nil && access.Settings.Security.AllowedHosts != nil {
		summary.AllowedHosts = access.Settings.Security.AllowedHosts
	}

	if err = checkSchema(fields); err != nil {
		return summary, err
	}

	return summary, nil
}

// parse разбирает файл импорта и проверяет, что это данные приложения.
func parse(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage

	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: файл не является JSON-объектом", ErrInvalidData)
	}

	if !slices.ContainsFunc(knownSections, func(key string) bool { return fields[key] != nil }) {
		return nil, fmt.Errorf("%w: в файле нет разделов app.json (группы, правила, outbound-ы, настройки)", ErrInvalidData)
	}

	return fields, nil
}

// countArray возвращает длину JSON-массива raw (0, если это не массив).
func countArray(raw json.RawMessage) int {
	var items []json.RawMessage

	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}

	return len(items)
}

// countNested возвращает длину массива key в JSON-объекте raw.
func countNested(raw json.RawMessage, key string) int {
	var object map[string]json.RawMessage

	if err := json.Unmarshal(raw, &object); err != nil {
		return 0
	}

	return countArray(object[key])
}

// Import проверяет данные data, приводит их к актуальной схеме и заменяет ими app.json. Прежние данные
// сохраняются в копию. После импорта запись в app.json запрещена до перезапуска приложения.
func (s *Service) Import(data []byte, opts ImportOptions) (*ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.imported {
		return nil, ErrImported
	}

	fields, err := parse(data)
	if err != nil {
		return nil, err
	}

	if err = checkSchema(fields); err != nil {
		return nil, err
	}

	migration, err := migrations.MigrateData(fields, s.singBox)
	if err != nil {
		return nil, fmt.Errorf("%w: миграция данных не удалась: %v", ErrInvalidData, err)
	}

	current, err := s.appData.ReadRaw()
	if err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read app data: %w", err)
	}

	if !opts.ReplaceAccess {
		if err = keepAccess(fields, current); err != nil {
			return nil, err
		}
	}

	if err = s.check(fields); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidData, err)
	}

	backup, err := s.backup(current)
	if err != nil {
		return nil, err
	}

	if err = s.appData.Replace(fields); err != nil {
		return nil, err
	}

	s.imported = true

	return &ImportResult{
		FromVersion: migration.FromVersion,
		ToVersion:   migration.ToVersion,
		Migrations:  migration.Applied,
		Backup:      backup,
	}, nil
}

// checkSchema проверяет, что версию схемы файла можно привести к актуальной: данные более новой версии
// приложения загружать нельзя.
func checkSchema(fields map[string]json.RawMessage) error {
	version, err := migrations.SchemaVersion(fields)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidData, err)
	}

	if latest := migrations.LatestVersion(); version > latest {
		return fmt.Errorf("%w: файл создан более новой версией конфигуратора (схема данных %d, эта версия поддерживает %d). "+
			"Обновите конфигуратор и повторите импорт", ErrUnsupportedSchema, version, latest)
	}

	if base := migrations.BaseVersion(); version < base {
		return fmt.Errorf("%w: файл создан слишком старой версией конфигуратора (схема данных %d, поддерживается с %d). "+
			"Загрузите его в версию v0.9.0, обновите ее и выгрузите данные заново", ErrUnsupportedSchema, version, base)
	}

	return nil
}

// keepAccess переносит в fields текущий вход в панель и защиту адреса панели из current. Если их в current
// нет, они удаляются и из fields: приложение создаст значения по умолчанию.
func keepAccess(fields, current map[string]json.RawMessage) error {
	if auth, ok := current[authKey]; ok {
		fields[authKey] = auth
	} else {
		delete(fields, authKey)
	}

	var imported, existing map[string]json.RawMessage

	if raw, ok := fields[settingsKey]; ok {
		if err := json.Unmarshal(raw, &imported); err != nil {
			return fmt.Errorf("%w: раздел settings: %v", ErrInvalidData, err)
		}
	}

	if raw, ok := current[settingsKey]; ok {
		_ = json.Unmarshal(raw, &existing)
	}

	if imported == nil {
		return nil
	}

	if security, ok := existing[securityKey]; ok {
		imported[securityKey] = security
	} else {
		delete(imported, securityKey)
	}

	raw, err := json.Marshal(imported)
	if err != nil {
		return fmt.Errorf("cannot marshal settings: %w", err)
	}

	fields[settingsKey] = raw

	return nil
}

// check записывает fields во временный файл рядом с app.json и проверяет, что менеджеры их загружают.
func (s *Service) check(fields map[string]json.RawMessage) error {
	file, err := os.CreateTemp(filepath.Dir(s.appData.Path()), ".app-import-*.json")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}

	path := file.Name()
	_ = file.Close()

	defer func() {
		_ = os.Remove(path)
	}()

	if err = appdata.NewFile(path).WriteRaw(fields); err != nil {
		return err
	}

	return s.validate(path)
}

// backup сохраняет текущие данные current в копию и возвращает путь к ней. Хранятся последние keepBackups копий.
func (s *Service) backup(current map[string]json.RawMessage) (string, error) {
	if current == nil {
		return "", nil
	}

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal app data: %w", err)
	}

	if err = os.MkdirAll(s.backupDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create backup dir: %w", err)
	}

	path := filepath.Join(s.backupDir, backupPrefix+time.Now().UTC().Format("20060102-150405.000")+".json")

	// В копии хэш пароля и ключи прокси: читать ее может только владелец.
	if err = fsutil.WriteFileAtomic(path, append(data, '\n'), 0600); err != nil {
		return "", fmt.Errorf("cannot write app data backup: %w", err)
	}

	s.pruneBackups()

	return path, nil
}

// pruneBackups удаляет старые копии app.json сверх keepBackups.
func (s *Service) pruneBackups() {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), backupPrefix) && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}

	// Имена содержат время, поэтому сортировка по имени — сортировка по времени.
	slices.Sort(names)

	for len(names) > keepBackups {
		_ = os.Remove(filepath.Join(s.backupDir, names[0]))
		names = names[1:]
	}
}

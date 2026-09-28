// Package singboxconfig читает и записывает рабочий конфиг sing-box и хранит его резервные копии.
package singboxconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/fsutil"
)

const (
	// backupPrefix — префикс имен файлов резервных копий конфига.
	backupPrefix = "sing-box-"

	// keepBackups — сколько последних резервных копий хранить.
	keepBackups = 10
)

// Provider работает с рабочим конфигом sing-box — тем, который sing-box читает при запуске.
// Файл меняется только атомарно при применении проверенного конфига.
type Provider struct {
	path      string
	backupDir string

	// Кэш секрета Clash API из рабочего конфига, сбрасывается при изменении файла.
	mu            sync.Mutex
	secretModTime time.Time
	secret        string
}

// NewProvider создает провайдер для конфига по пути path. Резервные копии хранятся в backupDir.
func NewProvider(path, backupDir string) *Provider {
	return &Provider{
		path:          path,
		backupDir:     backupDir,
		mu:            sync.Mutex{},
		secretModTime: time.Time{},
		secret:        "",
	}
}

// Path возвращает путь к рабочему конфигу.
func (p *Provider) Path() string {
	return p.path
}

// GetActualConfig возвращает содержимое рабочего конфига как есть. Если файла нет, ошибка оборачивает os.ErrNotExist.
func (p *Provider) GetActualConfig() ([]byte, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("cannot read sing-box config: %w", err)
	}

	return data, nil
}

// GetActualConfigParsed возвращает рабочий конфиг, разобранный в map. Комментарии удаляются.
func (p *Provider) GetActualConfigParsed() (map[string]any, error) {
	data, err := p.GetActualConfig()
	if err != nil {
		return nil, err
	}

	return Parse(data)
}

// WriteActualConfig атомарно записывает конфиг, заданный в виде map.
func (p *Provider) WriteActualConfig(config map[string]any) error {
	data, err := Marshal(config)
	if err != nil {
		return err
	}

	return p.Write(data)
}

// Write атомарно записывает рабочий конфиг.
func (p *Provider) Write(data []byte) error {
	if err := fsutil.WriteFileAtomic(p.path, data, 0644); err != nil {
		return fmt.Errorf("cannot write sing-box config: %w", err)
	}

	return nil
}

// Backup сохраняет копию рабочего конфига и возвращает путь к ней. Хранятся последние keepBackups копий.
// Если рабочего конфига еще нет, возвращает пустой путь.
func (p *Provider) Backup() (string, error) {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("cannot read sing-box config: %w", err)
	}

	if err = os.MkdirAll(p.backupDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create backup dir: %w", err)
	}

	backupPath := filepath.Join(p.backupDir, backupPrefix+time.Now().UTC().Format("20060102-150405.000")+".json")

	if err = fsutil.WriteFileAtomic(backupPath, data, 0600); err != nil {
		return "", fmt.Errorf("cannot write backup: %w", err)
	}

	p.pruneBackups()

	return backupPath, nil
}

// Restore записывает в рабочий конфиг содержимое резервной копии backupPath.
func (p *Provider) Restore(backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("cannot read backup: %w", err)
	}

	return p.Write(data)
}

// ClashSecret возвращает секрет Clash API из рабочего конфига (experimental.clash_api.secret).
// Результат кэшируется до изменения файла.
func (p *Provider) ClashSecret() string {
	info, err := os.Stat(p.path)
	if err != nil {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if info.ModTime().Equal(p.secretModTime) {
		return p.secret
	}

	p.secretModTime = info.ModTime()
	p.secret = ""

	config, err := p.GetActualConfigParsed()
	if err != nil {
		return ""
	}

	experimental, _ := config["experimental"].(map[string]any)
	clashAPI, _ := experimental["clash_api"].(map[string]any)
	p.secret, _ = clashAPI["secret"].(string)

	return p.secret
}

// pruneBackups удаляет старые резервные копии сверх keepBackups.
func (p *Provider) pruneBackups() {
	entries, err := os.ReadDir(p.backupDir)
	if err != nil {
		return
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), backupPrefix) {
			names = append(names, entry.Name())
		}
	}

	// Имена содержат время, поэтому сортировка по имени — сортировка по времени.
	slices.Sort(names)

	for len(names) > keepBackups {
		_ = os.Remove(filepath.Join(p.backupDir, names[0]))
		names = names[1:]
	}
}

// Parse разбирает конфиг sing-box. Комментарии (// и /* */) удаляются перед разбором.
func Parse(data []byte) (map[string]any, error) {
	var config map[string]any

	if err := json.Unmarshal(removeComments(data), &config); err != nil {
		return nil, fmt.Errorf("cannot parse sing-box config: %w", err)
	}

	return config, nil
}

// Marshal сериализует конфиг в тот вид, в котором он записывается на диск: с отступами, ключи
// объектов отсортированы, в конце перевод строки. Одинаковые конфиги дают одинаковые байты.
func Marshal(config map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot marshal sing-box config: %w", err)
	}

	return append(data, '\n'), nil
}

// multilineCommentRe — многострочные комментарии /* */.
var multilineCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

// removeComments удаляет из JSON комментарии /* */, // и #, не трогая содержимое строк.
func removeComments(data []byte) []byte {
	text := multilineCommentRe.ReplaceAllString(string(data), "")
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		inString := false
		escaped := false

		for j := 0; j < len(line); j++ {
			char := line[j]

			if escaped {
				escaped = false

				continue
			}

			if char == '\\' {
				escaped = true

				continue
			}

			if char == '"' {
				inString = !inString

				continue
			}

			if inString {
				continue
			}

			if char == '#' || (char == '/' && j+1 < len(line) && line[j+1] == '/') {
				lines[i] = line[:j]

				break
			}
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

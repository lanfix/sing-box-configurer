package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Фазы обновления.
const (
	PhaseRunning     = "running"
	PhaseRollingBack = "rolling_back"
	PhaseSucceeded   = "succeeded"
	PhaseRolledBack  = "rolled_back"
	PhaseFailed      = "failed"
)

// BackupFile описывает файл, сохраненный перед обновлением.
type BackupFile struct {
	// Path — путь относительно Target.Root.
	Path string `json:"path"`
}

// Journal — журнал обновления. Сохраняется перед каждым действием, чтобы после падения
// updater мог откатить изменения.
type Journal struct {
	ID          string `json:"id"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Phase       string `json:"phase"`
	Step        string `json:"step"`
	Error       string `json:"error,omitempty"`

	// Stopped — прежнюю версию могли остановить: при откате ее нужно вернуть.
	Stopped bool `json:"stopped"`

	// State — данные платформы для отката (контейнеры, пути бинарников).
	State map[string]string `json:"state"`

	Backups    []BackupFile `json:"backups"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`

	path string
}

// journalPath возвращает путь к журналу обновления.
func journalPath(updatesDir, id string) string {
	return filepath.Join(updatesDir, id, "journal.json")
}

// loadJournal загружает журнал. Если журнала нет, возвращается nil без ошибки.
func loadJournal(updatesDir, id string) (*Journal, error) {
	path := journalPath(updatesDir, id)

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("cannot read journal: %w", err)
	}

	var journal Journal

	if err = json.Unmarshal(raw, &journal); err != nil {
		return nil, fmt.Errorf("cannot parse journal: %w", err)
	}

	journal.path = path

	if journal.State == nil {
		journal.State = map[string]string{}
	}

	return &journal, nil
}

// newJournal создает журнал нового обновления.
func newJournal(updatesDir, id string) *Journal {
	return &Journal{
		ID:          id,
		FromVersion: "",
		ToVersion:   "",
		Phase:       PhaseRunning,
		Step:        "",
		Error:       "",
		Stopped:     false,
		State:       map[string]string{},
		Backups:     []BackupFile{},
		StartedAt:   time.Now().UTC(),
		FinishedAt:  nil,
		path:        journalPath(updatesDir, id),
	}
}

// Dir возвращает папку обновления.
func (j *Journal) Dir() string {
	return filepath.Dir(j.path)
}

// Save записывает журнал на диск через временный файл и rename (папка .updates не смонтирована отдельно).
func (j *Journal) Save() error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0755); err != nil {
		return fmt.Errorf("cannot create journal dir: %w", err)
	}

	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal journal: %w", err)
	}

	tmp := j.path + ".tmp"

	if err = os.WriteFile(tmp, append(raw, '\n'), 0644); err != nil {
		return fmt.Errorf("cannot write journal: %w", err)
	}

	if err = os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("cannot save journal: %w", err)
	}

	return nil
}

// Finish фиксирует итоговую фазу.
func (j *Journal) Finish(phase string, err error) error {
	j.Phase = phase
	j.FinishedAt = new(time.Now().UTC())

	if err != nil {
		j.Error = err.Error()
	}

	return j.Save()
}

// IsFinal возвращает true, если обновление завершено.
func (j *Journal) IsFinal() bool {
	switch j.Phase {
	case PhaseSucceeded, PhaseRolledBack, PhaseFailed:
		return true
	}

	return false
}

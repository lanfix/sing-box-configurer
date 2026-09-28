package happ

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// Profile описывает подписку Happ и её последнее известное состояние.
type Profile struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	URL        string       `json:"url"`
	Info       *ProfileInfo `json:"info,omitempty"`
	Servers    []Server     `json:"servers"`
	Warnings   []string     `json:"warnings,omitempty"`
	LastUpdate time.Time    `json:"last_update"`
	LastError  string       `json:"last_error,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

// storeData описывает данные Happ в app.json.
type storeData struct {
	InstallationID string    `json:"installation_id"`
	Profiles       []Profile `json:"profiles"`
}

// appDataSection описывает раздел app.json, которым владеет хранилище Happ.
type appDataSection struct {
	Happ storeData `json:"happ"`
}

// Store хранит ID инсталляции и профили Happ в разделе "happ" файла app.json.
type Store struct {
	appData *appdata.File
	mu      sync.RWMutex
	data    storeData
}

// NewStore загружает данные Happ из app.json. Если ID инсталляции ещё нет, он генерируется и сохраняется.
func NewStore(appData *appdata.File) (*Store, error) {
	s := &Store{
		appData: appData,
		mu:      sync.RWMutex{},
		data: storeData{
			InstallationID: "",
			Profiles:       []Profile{},
		},
	}

	var section appDataSection

	if err := appData.Read(&section); err != nil && !errors.Is(err, appdata.ErrNotExist) {
		return nil, fmt.Errorf("cannot read happ data: %w", err)
	}

	s.data = section.Happ

	if s.data.Profiles == nil {
		s.data.Profiles = []Profile{}
	}

	if s.data.InstallationID == "" {
		// Happ на iOS передает HWID в виде UUID в верхнем регистре.
		s.data.InstallationID = strings.ToUpper(uuid.NewString())

		if err := s.save(); err != nil {
			return nil, fmt.Errorf("cannot save installation id: %w", err)
		}
	}

	return s, nil
}

// InstallationID возвращает ID инсталляции, который используется как HWID устройства.
func (s *Store) InstallationID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data.InstallationID
}

// List возвращает копию списка профилей.
func (s *Store) List() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Profile, len(s.data.Profiles))
	copy(result, s.data.Profiles)

	return result
}

// Get возвращает профиль по ID.
func (s *Store) Get(id string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.data.Profiles {
		if s.data.Profiles[i].ID == id {
			return s.data.Profiles[i], true
		}
	}

	return Profile{}, false
}

// Put добавляет профиль или заменяет существующий с тем же ID.
func (s *Store) Put(profile Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Profiles {
		if s.data.Profiles[i].ID == profile.ID {
			s.data.Profiles[i] = profile

			return s.save()
		}
	}

	s.data.Profiles = append(s.data.Profiles, profile)

	return s.save()
}

// Delete удаляет профиль по ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Profiles {
		if s.data.Profiles[i].ID == id {
			s.data.Profiles = append(s.data.Profiles[:i], s.data.Profiles[i+1:]...)

			return s.save()
		}
	}

	return fmt.Errorf("profile not found")
}

// save записывает раздел Happ в app.json. Вызывается под блокировкой.
func (s *Store) save() error {
	section := appDataSection{
		Happ: s.data,
	}

	if err := s.appData.Merge(section); err != nil {
		return fmt.Errorf("cannot save happ data: %w", err)
	}

	return nil
}

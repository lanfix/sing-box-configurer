package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/fsutil"
)

// sourceIDRe — ID источника, из которого можно составить имя файла кэша.
var sourceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// cachedSource — последний успешно загруженный список источника на диске. После перезапуска
// конфигуратор сразу отдает его в rule-set-ах, не дожидаясь новой загрузки: иначе sing-box без
// своего кэша не запустится, а без sing-box может не загрузиться и сам источник (DNS, VPN).
type cachedSource struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	RuleSet   RuleSet   `json:"rule_set"`
}

// cachePath возвращает путь файла кэша источника. Пустая строка — кэш выключен или ID не годится для имени файла.
func (rm *Manager) cachePath(sourceID string) string {
	if rm.cacheDir == "" || !sourceIDRe.MatchString(sourceID) {
		return ""
	}

	return filepath.Join(rm.cacheDir, sourceID+".json")
}

// loadCachedSources загружает с диска списки примененных источников, у которых не менялась ссылка.
// Вызывается под блокировкой rm.mu.
func (rm *Manager) loadCachedSources() {
	loaded := 0

	rm.urlRulesMu.Lock()
	defer rm.urlRulesMu.Unlock()

	for _, source := range rm.data.URLSources {
		if !source.Applied || source.Deleted {
			continue
		}

		if _, ok := rm.urlRules[source.ID]; ok {
			continue
		}

		path := rm.cachePath(source.ID)
		if path == "" {
			continue
		}

		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		var cached cachedSource

		if err == nil {
			err = json.Unmarshal(raw, &cached)
		}

		if err != nil {
			log.Printf("Cannot read cached URL source %s: %v", source.Description, err)

			continue
		}

		if cached.URL != source.URL {
			continue
		}

		rm.urlRules[source.ID] = cached.RuleSet
		loaded++
	}

	if loaded > 0 {
		log.Printf("Loaded %d URL sources from cache %s", loaded, rm.cacheDir)
	}
}

// saveCachedSource записывает загруженный список источника на диск.
func (rm *Manager) saveCachedSource(sourceID, sourceURL string, ruleSet RuleSet) error {
	path := rm.cachePath(sourceID)
	if path == "" {
		return nil
	}

	raw, err := json.Marshal(cachedSource{
		URL:       sourceURL,
		FetchedAt: time.Now().UTC(),
		RuleSet:   ruleSet,
	})
	if err != nil {
		return fmt.Errorf("cannot marshal cached source: %w", err)
	}

	if err = os.MkdirAll(rm.cacheDir, 0755); err != nil {
		return fmt.Errorf("cannot create url sources cache dir: %w", err)
	}

	return fsutil.WriteFileAtomic(path, raw, 0644)
}

// removeCachedSource удаляет кэш удаленного источника.
func (rm *Manager) removeCachedSource(sourceID string) {
	if path := rm.cachePath(sourceID); path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("Cannot remove cached URL source %s: %v", sourceID, err)
		}
	}
}

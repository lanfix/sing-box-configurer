package devices

import (
	"context"
	"sync"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/platform"
)

const (
	// supportRetry — не чаще этого рендер повторяет проверку после неудачной (например, sing-box не запущен).
	supportRetry = 5 * time.Second

	// supportTimeout ограничивает проверку по запросу при рендере конфига.
	supportTimeout = 30 * time.Second
)

// probeConfig — минимальный конфиг с MAC-адресом в rule-set: его принимает только sing-box с поддержкой
// source_mac_address в rule-set-ах (sing-box-lx с SPEC 113).
var probeConfig = []byte(`{
  "log": {"disabled": true},
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {
    "find_neighbor": true,
    "rule_set": [{"type": "inline", "tag": "probe", "rules": [{"source_mac_address": ["02:00:00:00:00:01"]}]}],
    "rules": [{"rule_set": "probe", "outbound": "direct"}]
  }
}`)

// VersionSource возвращает версию запущенного sing-box (Clash API).
type VersionSource interface {
	GetVersion() (string, error)
}

// Support — поддерживает ли sing-box профили устройств.
type Support struct {
	// Known — проверка выполнена, Supported достоверен.
	Known     bool      `json:"known"`
	Supported bool      `json:"supported"`
	Version   string    `json:"version"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// Enabled сообщает, что профили устройств можно отдавать sing-box: проверка прошла и поддержка есть.
func (s Support) Enabled() bool {
	return s.Known && s.Supported
}

// supportChecker проверяет поддержку командой sing-box check. Хранит результат последней удачной проверки,
// а после неудачной (sing-box не запущен) перепроверяет при первом запросе.
type supportChecker struct {
	checker  platform.SingBox
	versions VersionSource

	mu        sync.Mutex
	support   Support
	failed    bool
	attemptAt time.Time
}

// get возвращает результат последней удачной проверки.
func (c *supportChecker) get() Support {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.support
}

// refresh проверяет поддержку, если сменилась версия sing-box, Clash API недоступен (бинарник могли заменить,
// а sing-box не запуститься) или прошлая проверка не удалась.
func (c *supportChecker) refresh(ctx context.Context) Support {
	if c.checker == nil {
		return c.get()
	}

	version := ""

	if c.versions != nil {
		version, _ = c.versions.GetVersion()
	}

	c.mu.Lock()
	current := c.support.Known && !c.failed && version != "" && version == c.support.Version
	c.mu.Unlock()

	if current {
		return c.get()
	}

	return c.probe(ctx, version)
}

// ensure проверяет поддержку сразу, если результата еще нет или прошлая проверка не удалась, но не чаще retry.
// Clash API не запрашивается: sing-box может как раз запускаться.
func (c *supportChecker) ensure(ctx context.Context, retry time.Duration) Support {
	if c.checker == nil {
		return c.get()
	}

	c.mu.Lock()
	settled := (c.support.Known && !c.failed) || time.Since(c.attemptAt) < retry
	c.mu.Unlock()

	if settled {
		return c.get()
	}

	return c.probe(ctx, "")
}

// probe выполняет проверку. Если sing-box проверить не удалось, остается результат прошлой удачной проверки.
func (c *supportChecker) probe(ctx context.Context, version string) Support {
	c.mu.Lock()
	c.attemptAt = time.Now()
	c.mu.Unlock()

	result, err := c.checker.Check(ctx, probeConfig)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.failed = true
		c.support.Error = "не удалось проверить sing-box: " + err.Error()

		return c.support
	}

	c.failed = false
	c.support = Support{
		Known:     true,
		Supported: result.ExitCode == 0,
		Version:   version,
		Error:     "",
		CheckedAt: time.Now(),
	}

	return c.support
}

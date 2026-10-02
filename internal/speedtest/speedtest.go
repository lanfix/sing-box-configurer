// Package speedtest замеряет скорость загрузки через outbound sing-box: файл скачивается с тестового сервера
// через служебный mixed-inbound sing-box (логин — тег outbound-а). Тестовые серверы бывают заблокированы
// в стране роутера или в стране выхода outbound-а, поэтому серверы перебираются по списку до первого,
// который отвечает.
package speedtest

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lanfix/sing-box-configurer/internal/settings"
)

const (
	// probeTimeout — сколько ждать ответа сервера и первых байтов файла. Сервер, который не ответил,
	// считается недоступным через этот outbound, и тест переходит к следующему.
	probeTimeout = 10 * time.Second

	// lowDataThreshold — меньше этого объема за замер — повод предупредить об ограничении скорости.
	lowDataThreshold = 256 << 10
)

var (
	// ErrBusy возвращается, если тест скорости уже выполняется: параллельные замеры мешают друг другу.
	ErrBusy = errors.New("тест скорости уже выполняется")

	// ErrUnknownServer возвращается, если запрошенного сервера нет в настройках.
	ErrUnknownServer = errors.New("сервера нет в настройках теста скорости")

	// errProxyUnavailable — служебный inbound не принял соединение: другие серверы пробовать бесполезно.
	errProxyUnavailable = errors.New("служебный inbound недоступен")
)

// ProxyFunc возвращает адрес служебного inbound-а sing-box для outbound-а tag.
type ProxyFunc func(tag string) (*url.URL, error)

// SettingsFunc возвращает текущие настройки теста скорости.
type SettingsFunc func() settings.SpeedTest

// Attempt — сервер, который не подошел для замера.
type Attempt struct {
	Server string `json:"server"`
	Error  string `json:"error"`
}

// Result — итог замера скорости через outbound.
type Result struct {
	Tag string `json:"tag"`

	// Server и ServerURL — сервер, с которого шла загрузка.
	Server    string `json:"server,omitempty"`
	ServerURL string `json:"server_url,omitempty"`

	// Download — скорость загрузки, байт/с. Bytes — скачано за замер.
	Download   float64 `json:"download"`
	Bytes      int64   `json:"bytes"`
	DurationMs int64   `json:"duration_ms"`

	// LatencyMs — время до первого байта файла: подключение через outbound, TLS и ответ сервера.
	LatencyMs int64 `json:"latency_ms"`

	// Attempts — серверы, которые не ответили через outbound, по порядку.
	Attempts []Attempt `json:"attempts"`

	// Warning — замер прошел, но результат сомнительный. Error — замер не удался.
	Warning string `json:"warning,omitempty"`
	Error   string `json:"error,omitempty"`

	TestedAt time.Time `json:"tested_at"`
}

// Service выполняет замеры и хранит последний результат каждого outbound-а до перезапуска.
type Service struct {
	proxy    ProxyFunc
	settings SettingsFunc

	mu      sync.Mutex
	running string
	results map[string]Result
}

// NewService создает сервис теста скорости.
func NewService(proxy ProxyFunc, settings SettingsFunc) *Service {
	return &Service{
		proxy:    proxy,
		settings: settings,
		mu:       sync.Mutex{},
		running:  "",
		results:  map[string]Result{},
	}
}

// Status возвращает outbound, замер которого идет сейчас, и последние результаты.
func (s *Service) Status() (string, map[string]Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make(map[string]Result, len(s.results))

	for tag, result := range s.results {
		results[tag] = result
	}

	return s.running, results
}

// Run замеряет скорость загрузки через outbound tag. serverURL — сервер из настроек; пустой — первый
// доступный по списку. Неудачный замер — не ошибка: причина в Result.Error.
func (s *Service) Run(ctx context.Context, tag, serverURL string) (*Result, error) {
	cfg := s.settings()
	servers := cfg.Servers

	if serverURL != "" {
		servers = nil

		for _, server := range cfg.Servers {
			if server.URL == serverURL {
				servers = []settings.SpeedTestServer{server}
			}
		}

		if servers == nil {
			return nil, ErrUnknownServer
		}
	}

	proxyURL, err := s.proxy(tag)
	if err != nil {
		return nil, err
	}

	if !s.begin(tag) {
		return nil, ErrBusy
	}

	defer s.end()

	result := s.measure(ctx, tag, proxyURL, servers, cfg)

	s.mu.Lock()
	s.results[tag] = result
	s.mu.Unlock()

	return &result, nil
}

// begin отмечает начало замера. Возвращает false, если замер уже идет.
func (s *Service) begin(tag string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running != "" {
		return false
	}

	s.running = tag

	return true
}

// end отмечает окончание замера.
func (s *Service) end() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = ""
}

// measure перебирает серверы до первого, который отвечает через outbound, и замеряет на нем скорость.
func (s *Service) measure(ctx context.Context, tag string, proxyURL *url.URL, servers []settings.SpeedTestServer, cfg settings.SpeedTest) Result {
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		TLSHandshakeTimeout:   probeTimeout,
		ResponseHeaderTimeout: probeTimeout,
		DisableCompression:    true,

		// HTTP/2 уложил бы все потоки в одно соединение: каждому потоку нужно свое.
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}

	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
	}

	result := Result{
		Tag:      tag,
		Attempts: []Attempt{},
		TestedAt: time.Now(),
	}

	// outboundFailures — серверы, соединение с которыми закрыл sing-box: outbound не подключился к ним.
	outboundFailures := 0

	for _, server := range servers {
		attemptCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(probeTimeout, cancel)

		start := time.Now()
		body, first, err := probe(attemptCtx, client, server.URL)

		// Таймер успел сработать — сервер не ответил вовремя, даже если ответ пришел следом.
		if !timer.Stop() && err == nil {
			_ = body.Close()
			err = fmt.Errorf("нет ответа за %s", probeTimeout)
		}

		if err != nil {
			cancel()

			if ctx.Err() != nil {
				result.Error = "Замер прерван"

				return result
			}

			if errors.Is(err, errProxyUnavailable) {
				result.Error = describeProxyError(err, tag)

				return result
			}

			description := describeError(err)

			if isOutboundFailure(err, proxyURL.Host) {
				outboundFailures++
				description = "outbound не подключился к серверу: sing-box закрыл соединение"
			}

			result.Attempts = append(result.Attempts, Attempt{
				Server: server.Name,
				Error:  description,
			})

			continue
		}

		latency := time.Since(start)
		total, elapsed := download(attemptCtx, cancel, client, server.URL, body, first, cfg)

		cancel()

		result.Server = server.Name
		result.ServerURL = server.URL
		result.Bytes = total
		result.DurationMs = elapsed.Milliseconds()
		result.LatencyMs = latency.Milliseconds()

		if elapsed > 0 {
			result.Download = float64(total) / elapsed.Seconds()
		}

		if total < lowDataThreshold {
			result.Warning = "Скачано почти ничего: сервер или outbound ограничивает скорость. Попробуйте другой сервер."
		}

		return result
	}

	switch {
	case outboundFailures == len(servers):
		result.Error = "Outbound не устанавливает соединения: sing-box закрывает подключения ко всем тестовым серверам. " +
			"Проверьте сервер outbound-а, например замером задержки на странице «Прокси»"

	case len(servers) == 1:
		result.Error = "Сервер не ответил через этот outbound"

	default:
		result.Error = "Ни один сервер из списка не ответил через этот outbound"
	}

	return result
}

// isOutboundFailure проверяет, что соединение через служебный inbound (адрес proxyHost) закрыл sing-box:
// так он отвечает, если outbound не смог подключиться к серверу.
func isOutboundFailure(err error, proxyHost string) bool {
	message := err.Error()

	if strings.Contains(message, proxyHost) && (strings.Contains(message, "reset by peer") || strings.Contains(message, "broken pipe")) {
		return true
	}

	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// probe запрашивает файл и дожидается первых байтов. Возвращает тело ответа и число прочитанных байтов.
func probe(ctx context.Context, client *http.Client, rawURL string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		if isProxyFailure(err) {
			return nil, 0, fmt.Errorf("%w: %v", errProxyUnavailable, err)
		}

		return nil, 0, err
	}

	if resp.StatusCode == http.StatusProxyAuthRequired {
		_ = resp.Body.Close()

		return nil, 0, fmt.Errorf("%w: %s", errProxyUnavailable, resp.Status)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		_ = resp.Body.Close()

		return nil, 0, fmt.Errorf("сервер ответил %s", resp.Status)
	}

	buf := make([]byte, 32<<10)

	n, err := resp.Body.Read(buf)
	if n == 0 {
		_ = resp.Body.Close()

		if err == nil || errors.Is(err, io.EOF) {
			return nil, 0, errors.New("сервер вернул пустой ответ")
		}

		return nil, 0, err
	}

	return resp.Body, int64(n), nil
}

// download скачивает файл в cfg.Streams потоков в течение cfg.Duration секунд. Первый поток продолжает
// чтение тела body, остальные запрашивают файл заново; файл, скачанный до конца, запрашивается снова.
// Возвращает скачанный объем и время замера.
func download(
	ctx context.Context, cancel context.CancelFunc, client *http.Client, rawURL string, body io.ReadCloser, first int64,
	cfg settings.SpeedTest,
) (int64, time.Duration) {
	var total atomic.Int64

	total.Add(first)

	start := time.Now()
	timer := time.AfterFunc(time.Duration(cfg.Duration)*time.Second, cancel)

	defer timer.Stop()

	counter := &countingWriter{
		total: &total,
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		_, _ = io.Copy(counter, body)
		_ = body.Close()

		repeat(ctx, client, rawURL, counter)
	})

	for range cfg.Streams - 1 {
		wg.Go(func() {
			repeat(ctx, client, rawURL, counter)
		})
	}

	wg.Wait()

	return total.Load(), min(time.Since(start), time.Duration(cfg.Duration)*time.Second)
}

// repeat скачивает файл раз за разом, пока не закончится замер. Ошибка запроса завершает поток.
func repeat(ctx context.Context, client *http.Client, rawURL string, counter io.Writer) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			return
		}

		_, _ = io.Copy(counter, resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return
		}
	}
}

// countingWriter считает записанные байты.
type countingWriter struct {
	total *atomic.Int64
}

// Write учитывает len(p) байт.
func (w *countingWriter) Write(p []byte) (int, error) {
	w.total.Add(int64(len(p)))

	return len(p), nil
}

// isProxyFailure проверяет, что ошибка — отказ служебного inbound-а: он не принимает соединения или не знает
// пользователя (outbound-а нет в рабочем конфиге).
func isProxyFailure(err error) bool {
	message := err.Error()

	return strings.Contains(message, "proxyconnect") || strings.Contains(message, "Proxy Authentication Required")
}

// describeProxyError объясняет отказ служебного inbound-а.
func describeProxyError(err error, tag string) string {
	if strings.Contains(err.Error(), "Proxy Authentication Required") || strings.Contains(err.Error(), "407") {
		return fmt.Sprintf("Outbound-а %s нет в служебном inbound-е рабочего конфига sing-box: примените конфиг", tag)
	}

	return "Служебный inbound sing-box не принимает соединения: проверьте, что sing-box запущен и конфиг применен"
}

// describeError возвращает текст ошибки запроса без адреса.
func describeError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Sprintf("нет ответа за %s", probeTimeout)
	}

	var urlErr *url.Error

	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return fmt.Sprintf("нет ответа за %s", probeTimeout)
		}

		return urlErr.Err.Error()
	}

	return err.Error()
}

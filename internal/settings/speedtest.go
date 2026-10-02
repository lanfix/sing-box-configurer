package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Ограничения настроек теста скорости.
const (
	minSpeedTestDuration = 3
	maxSpeedTestDuration = 60
	minSpeedTestStreams  = 1
	maxSpeedTestStreams  = 16
	maxSpeedTestServers  = 20
)

// SpeedTestServer — сервер, с которого тест скорости скачивает файл.
type SpeedTestServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// SpeedTest — тест скорости outbound-ов: файл скачивается с первого доступного через outbound сервера
// списка. Серверы могут быть заблокированы в стране, где стоит роутер, или в стране выхода outbound-а,
// поэтому их несколько.
type SpeedTest struct {
	Servers []SpeedTestServer `json:"servers"`

	// Duration — длительность замера, с.
	Duration int `json:"duration"`

	// Streams — число параллельных загрузок: одно соединение часто не загружает канал полностью.
	Streams int `json:"streams"`
}

// DefaultSpeedTest возвращает настройки теста скорости по умолчанию: серверы в разных сетях и странах.
// Cloudflare не первый: он ограничивает частые загрузки (429), а Hetzner недоступен напрямую из России.
func DefaultSpeedTest() SpeedTest {
	return SpeedTest{
		Servers: []SpeedTestServer{
			{Name: "OVH (Франция)", URL: "https://proof.ovh.net/files/100Mb.dat"},
			{Name: "Linode (Германия)", URL: "https://speedtest.frankfurt.linode.com/100MB-frankfurt.bin"},
			{Name: "Cloudflare", URL: "https://speed.cloudflare.com/__down?bytes=100000000"},
			{Name: "Hetzner (Германия)", URL: "https://fsn1-speed.hetzner.com/100MB.bin"},
			{Name: "Selectel (Россия)", URL: "https://speedtest.selectel.ru/100MB"},
		},
		Duration: 10,
		Streams:  4,
	}
}

// Normalize проверяет настройки и возвращает их без лишних пробелов.
func (s SpeedTest) Normalize() (SpeedTest, error) {
	if s.Duration < minSpeedTestDuration || s.Duration > maxSpeedTestDuration {
		return SpeedTest{}, fmt.Errorf("длительность замера — от %d до %d секунд", minSpeedTestDuration, maxSpeedTestDuration)
	}

	if s.Streams < minSpeedTestStreams || s.Streams > maxSpeedTestStreams {
		return SpeedTest{}, fmt.Errorf("число потоков — от %d до %d", minSpeedTestStreams, maxSpeedTestStreams)
	}

	if len(s.Servers) == 0 {
		return SpeedTest{}, errors.New("нужен хотя бы один сервер теста скорости")
	}

	if len(s.Servers) > maxSpeedTestServers {
		return SpeedTest{}, fmt.Errorf("серверов теста скорости — не больше %d", maxSpeedTestServers)
	}

	servers := make([]SpeedTestServer, 0, len(s.Servers))
	seen := map[string]bool{}

	for _, server := range s.Servers {
		server.Name = strings.TrimSpace(server.Name)
		server.URL = strings.TrimSpace(server.URL)

		parsed, err := url.Parse(server.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return SpeedTest{}, fmt.Errorf("адрес сервера %q должен быть ссылкой http:// или https://", server.URL)
		}

		if server.Name == "" {
			server.Name = parsed.Hostname()
		}

		if seen[server.URL] {
			return SpeedTest{}, fmt.Errorf("сервер %s указан дважды", server.URL)
		}

		seen[server.URL] = true
		servers = append(servers, server)
	}

	return SpeedTest{
		Servers:  servers,
		Duration: s.Duration,
		Streams:  s.Streams,
	}, nil
}

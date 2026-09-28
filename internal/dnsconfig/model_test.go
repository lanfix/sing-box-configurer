package dnsconfig

import (
	"encoding/json"
	"testing"
)

func TestServerRoundTrip(t *testing.T) {
	configs := []string{
		`{"neighbor_domain":[".",".lan"],"tag":"local","type":"local"}`,
		`{"server":"77.88.8.1","tag":"yandex","type":"udp"}`,
		`{"detour":"select-default","path":"/dns-query","server":"1.1.1.1","server_port":443,"tag":"cloudflare","tls":{"server_name":"cloudflare-dns.com"},"type":"https"}`,
		`{"server":"8.8.8.8","tag":"google","tls":{"alpn":["h2"],"insecure":true,"server_name":"dns.google"},"type":"tls"}`,
	}

	for _, raw := range configs {
		var config map[string]any

		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}

		server := ServerFromConfig(config)

		if err := server.Validate(); err != nil {
			t.Errorf("%s: %v", raw, err)
		}

		got, err := json.Marshal(server.Config())
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != raw {
			t.Errorf("round trip:\n got %s\nwant %s", got, raw)
		}
	}
}

func TestServerValidate(t *testing.T) {
	cases := []Server{
		{Tag: "", Type: TypeUDP, Server: "1.1.1.1"},
		{Tag: "x", Type: "doh", Server: "1.1.1.1"},
		{Tag: "x", Type: TypeHTTPS, Server: ""},
		{Tag: "configurer-hosts", Type: TypeLocal},
		{Tag: "x", Type: TypeUDP, Server: "1.1.1.1", DomainResolver: "x"},
	}

	for _, server := range cases {
		if err := server.Validate(); err == nil {
			t.Errorf("%+v: expected error", server)
		}
	}
}

func TestSettingsValidate(t *testing.T) {
	settings := Settings{Final: "yandex", Strategy: "ipv4_only", Timeout: "5s"}

	if err := settings.Validate([]string{"yandex"}); err != nil {
		t.Fatal(err)
	}

	if err := settings.Validate([]string{"local"}); err == nil {
		t.Error("unknown final: expected error")
	}

	settings.Timeout = "5 sec"

	if err := settings.Validate([]string{"yandex"}); err == nil {
		t.Error("bad timeout: expected error")
	}
}

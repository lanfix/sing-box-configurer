package outbound

import (
	"encoding/json"
	"testing"
)

func TestShareTransport(t *testing.T) {
	cases := map[string]string{
		"trojan://pass@example.com:443?security=tls&type=ws&path=%2Fcdn&host=cdn.example.com#WS": `{"headers":{"Host":"cdn.example.com"},"path":"/cdn","type":"ws"}`,
		"vless://uuid@example.com:443?security=tls&type=grpc&serviceName=svc#GRPC":               `{"service_name":"svc","type":"grpc"}`,
		"vless://uuid@example.com:443?security=tls&type=httpupgrade&path=%2Fup&host=h.com#HU":    `{"host":"h.com","path":"/up","type":"httpupgrade"}`,
		"vless://uuid@example.com:443?security=tls&type=tcp#TCP":                                 `null`,
	}

	for link, want := range cases {
		share, err := ParseShareUrl(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}

		raw, err := json.Marshal(share.GetOutbound().Config["transport"])
		if err != nil {
			t.Fatal(err)
		}

		if string(raw) != want {
			t.Errorf("%s:\n got %s\nwant %s", link, raw, want)
		}
	}

	hop, err := ParseShareUrl("hysteria2://pass@example.com:20000-29999?sni=example.com#HOP")
	if err != nil {
		t.Fatal(err)
	}

	if raw, _ := json.Marshal(hop.GetOutbound().Config["server_ports"]); string(raw) != `["20000:29999"]` {
		t.Errorf("server_ports = %s, want [\"20000:29999\"]", raw)
	}

	if _, err := ParseShareUrl("vless://uuid@example.com:443?type=xhttp#X"); err == nil {
		t.Error("xhttp must be rejected: sing-box does not support it")
	}
}

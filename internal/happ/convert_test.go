package happ

import (
	"encoding/base64"
	"net/http"
	"testing"
)

const testXraySubscription = `[
  {
    "remarks": "Fastest",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "de.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111", "encryption": "none", "flow": "xtls-rprx-vision"}]}]},
       "streamSettings": {"network": "tcp", "security": "reality", "realitySettings": {"serverName": "sni.example", "publicKey": "PUBKEY", "shortId": "abcd", "fingerprint": "firefox"}}},
      {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "fi.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111", "encryption": "none", "flow": "xtls-rprx-vision"}]}]},
       "streamSettings": {"network": "tcp", "security": "reality", "realitySettings": {"serverName": "sni.example", "publicKey": "PUBKEY", "shortId": "abcd", "fingerprint": "firefox"}}},
      {"tag": "direct", "protocol": "freedom"},
      {"tag": "block", "protocol": "blackhole"}
    ],
    "routing": {"balancers": [{"tag": "auto", "selector": ["proxy"], "strategy": {"type": "leastPing"}}]}
  },
  {
    "remarks": "Germany",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "de.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111", "encryption": "none", "flow": "xtls-rprx-vision"}]}]},
       "streamSettings": {"network": "tcp", "security": "reality", "realitySettings": {"serverName": "sni.example", "publicKey": "PUBKEY", "shortId": "abcd", "fingerprint": "firefox"}}}
    ]
  },
  {
    "remarks": "WS",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "ws.example.com", "port": 8443, "users": [{"id": "11111111-1111-1111-1111-111111111111", "encryption": "none", "flow": ""}]}]},
       "streamSettings": {"network": "ws", "wsSettings": {"path": "/ws?ed=2048", "host": "cdn.example.com"}, "security": "tls", "tlsSettings": {"serverName": "cdn.example.com", "fingerprint": "chrome", "alpn": ["http/1.1"]}}}
    ]
  },
  {
    "remarks": "gRPC",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "grpc.example.com", "port": 8444, "users": [{"id": "11111111-1111-1111-1111-111111111111", "encryption": "none"}]}]},
       "streamSettings": {"network": "grpc", "grpcSettings": {"serviceName": "svc"}, "security": "reality", "realitySettings": {"serverName": "grpc.example.com", "publicKey": "PUBKEY2", "shortId": "ef"}}}
    ]
  },
  {
    "remarks": "XHTTP",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "x.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]},
       "streamSettings": {"network": "xhttp", "security": "tls"}}
    ]
  }
]`

// TestParseSubscriptionXray проверяет разбор xray JSON: дедупликацию, имена, транспорты и urltest.
func TestParseSubscriptionXray(t *testing.T) {
	result, err := ParseSubscription([]byte(testXraySubscription), "Test")
	if err != nil {
		t.Fatal(err)
	}

	byTag := map[string]Server{}

	for _, server := range result.Servers {
		byTag[server.Tag] = server
	}

	wantTags := []string{"[Test] Germany", "[Test] WS", "[Test] gRPC", "[Test] fi.example.com", "[Test] Fastest"}
	if len(result.Servers) != len(wantTags) {
		t.Fatalf("got %d servers, want %d: %+v", len(result.Servers), len(wantTags), result.Servers)
	}

	for i, tag := range wantTags {
		if result.Servers[i].Tag != tag {
			t.Errorf("server[%d].Tag = %q, want %q", i, result.Servers[i].Tag, tag)
		}
	}

	if len(result.Warnings) != 1 {
		t.Errorf("got warnings %v, want 1 (xhttp)", result.Warnings)
	}

	germany := byTag["[Test] Germany"].Outbound
	tls := germany["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)

	if germany["flow"] != "xtls-rprx-vision" || reality["public_key"] != "PUBKEY" || reality["short_id"] != "abcd" {
		t.Errorf("unexpected reality outbound: %+v", germany)
	}

	if tls["utls"].(map[string]any)["fingerprint"] != "firefox" {
		t.Errorf("unexpected utls: %+v", tls["utls"])
	}

	ws := byTag["[Test] WS"].Outbound["transport"].(map[string]any)
	if ws["path"] != "/ws" || ws["max_early_data"] != 2048 || ws["headers"].(map[string]any)["Host"] != "cdn.example.com" {
		t.Errorf("unexpected ws transport: %+v", ws)
	}

	grpc := byTag["[Test] gRPC"].Outbound
	if grpc["transport"].(map[string]any)["service_name"] != "svc" {
		t.Errorf("unexpected grpc transport: %+v", grpc["transport"])
	}

	if grpc["tls"].(map[string]any)["utls"].(map[string]any)["fingerprint"] != "chrome" {
		t.Errorf("reality without fingerprint must fall back to chrome utls")
	}

	fastest := byTag["[Test] Fastest"]
	members := fastest.Outbound["outbounds"].([]string)

	if fastest.Type != ServerTypeURLTest || len(members) != 2 || members[0] != "[Test] Germany" || members[1] != "[Test] fi.example.com" {
		t.Errorf("unexpected urltest: %+v", fastest.Outbound)
	}
}

// TestParseSubscriptionShareLinks проверяет разбор base64-подписки со share-ссылками.
func TestParseSubscriptionShareLinks(t *testing.T) {
	links := "vless://11111111-1111-1111-1111-111111111111@de.example.com:443?type=tcp&security=reality&sni=sni.example&fp=firefox&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision#DE\n" +
		"vless://11111111-1111-1111-1111-111111111111@de.example.com:443?type=tcp&security=reality&sni=sni.example&fp=firefox&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision#DE-dup\n"

	result, err := ParseSubscription([]byte(base64.StdEncoding.EncodeToString([]byte(links))), "P")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Servers) != 1 || result.Servers[0].Tag != "[P] DE" {
		t.Fatalf("unexpected servers: %+v", result.Servers)
	}

	if result.Servers[0].Outbound["tag"] != "[P] DE" {
		t.Errorf("outbound tag not set: %+v", result.Servers[0].Outbound)
	}
}

// TestParseSubscriptionStub проверяет, что заглушка панели (лимит устройств) считается ошибкой.
func TestParseSubscriptionStub(t *testing.T) {
	stub := `[{"remarks": "Disabled", "outbounds": [{"tag": "x", "protocol": "vless", "settings": {"vnext": [{"address": "device-disabled.invalid", "port": 443, "users": [{"id": "00000000-0000-4000-8000-000000000000"}]}]}}]}]`

	if _, err := ParseSubscription([]byte(stub), "P"); err == nil {
		t.Fatal("expected error for stub subscription")
	}
}

// TestParseProfileInfo проверяет разбор заголовков подписки.
func TestParseProfileInfo(t *testing.T) {
	header := http.Header{}
	header.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Null VPN")))
	header.Set("Subscription-Userinfo", "upload=10; download=120549883407; total=0; expire=1790970491")
	header.Set("Profile-Update-Interval", "3")

	info := parseProfileInfo(header)

	if info.Title != "Null VPN" || info.Upload != 10 || info.Download != 120549883407 || info.Total != 0 || info.UpdateInterval != 3 {
		t.Errorf("unexpected info: %+v", info)
	}

	if info.Expire == nil || info.Expire.Unix() != 1790970491 {
		t.Errorf("unexpected expire: %v", info.Expire)
	}
}

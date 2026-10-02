package outbound

import (
	"encoding/json"
	"testing"
)

// TestShareShadowsocks проверяет разбор ссылок ss:// в формате SIP002 и в старом формате.
func TestShareShadowsocks(t *testing.T) {
	cases := map[string]string{
		// SIP002: userinfo в base64, имя в URL-кодировке.
		"ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpzZWNyZXQgcGFzcw==@ss.example.com:8388#%F0%9F%87%A9%F0%9F%87%AA%20DE": `{"method":"chacha20-ietf-poly1305","password":"secret pass","server":"ss.example.com","server_port":8388,"tag":"🇩🇪 DE","type":"shadowsocks"}`,

		// SIP002: метод 2022 и ключ как есть, IPv6, плагин.
		"ss://2022-blake3-aes-256-gcm:a2V5%3D@[2001:db8::1]:443/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dcdn.example.com#v6": `{"method":"2022-blake3-aes-256-gcm","password":"a2V5=","plugin":"obfs-local","plugin_opts":"obfs=http;obfs-host=cdn.example.com","server":"2001:db8::1","server_port":443,"tag":"v6","type":"shadowsocks"}`,

		// Старый формат: все в base64, имени нет.
		"ss://YWVzLTI1Ni1nY206cHdkQDEuMi4zLjQ6ODM4OA==": `{"method":"aes-256-gcm","password":"pwd","server":"1.2.3.4","server_port":8388,"tag":"ss-1.2.3.4","type":"shadowsocks"}`,
	}

	for link, want := range cases {
		share, err := ParseShareUrl(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}

		raw, err := json.Marshal(share.GetOutbound().Config)
		if err != nil {
			t.Fatal(err)
		}

		if string(raw) != want {
			t.Errorf("%s:\n got %s\nwant %s", link, raw, want)
		}
	}

	for _, link := range []string{
		"ss://cmM0OnB3ZA@example.com:8388",
		"ss://YWVzLTI1Ni1nY206cHdk@example.com:8388/?plugin=kcptun",
		"ss://YWVzLTI1Ni1nY206cHdk@example.com",
	} {
		if _, err := ParseShareUrl(link); err == nil {
			t.Errorf("%s must be rejected", link)
		}
	}
}

// TestShareVMess проверяет разбор ссылок vmess:// в формате v2rayN и в формате uuid@host:port.
func TestShareVMess(t *testing.T) {
	cases := map[string]string{
		"vmess://eyJ2IjoiMiIsInBzIjoiVk1lc3MgV1MiLCJhZGQiOiJ2bS5leGFtcGxlLmNvbSIsInBvcnQiOiI0NDMiLCJpZCI6IjExMTExMTExLTIyMjItMzMzMy00NDQ0LTU1NTU1NTU1NTU1NSIsImFpZCI6IjAiLCJzY3kiOiJhdXRvIiwibmV0Ijoid3MiLCJ0eXBlIjoibm9uZSIsImhvc3QiOiJjZG4uZXhhbXBsZS5jb20iLCJwYXRoIjoiL3dzIiwidGxzIjoidGxzIiwic25pIjoiIiwiYWxwbiI6ImgyLGh0dHAvMS4xIiwiZnAiOiJjaHJvbWUifQ==": `{"alter_id":0,"security":"auto","server":"vm.example.com","server_port":443,"tag":"VMess WS","tls":{"alpn":["h2","http/1.1"],"enabled":true,"server_name":"cdn.example.com","utls":{"enabled":true,"fingerprint":"chrome"}},"transport":{"headers":{"Host":"cdn.example.com"},"path":"/ws","type":"ws"},"type":"vmess","uuid":"11111111-2222-3333-4444-555555555555"}`,

		// Числа вместо строк, gRPC без TLS.
		"vmess://eyJwcyI6ImdycGMiLCJhZGQiOiJnLmV4YW1wbGUuY29tIiwicG9ydCI6NDQzLCJpZCI6InUiLCJhaWQiOjIsIm5ldCI6ImdycGMiLCJwYXRoIjoic3ZjIiwidGxzIjoiIn0=": `{"alter_id":2,"security":"auto","server":"g.example.com","server_port":443,"tag":"grpc","transport":{"service_name":"svc","type":"grpc"},"type":"vmess","uuid":"u"}`,

		"vmess://uuid-1@vm.example.com:8443?encryption=aes-128-gcm&security=tls&sni=sni.example.com&type=httpupgrade&path=%2Fup#URI": `{"alter_id":0,"security":"aes-128-gcm","server":"vm.example.com","server_port":8443,"tag":"URI","tls":{"enabled":true,"server_name":"sni.example.com"},"transport":{"path":"/up","type":"httpupgrade"},"type":"vmess","uuid":"uuid-1"}`,
	}

	for link, want := range cases {
		share, err := ParseShareUrl(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}

		raw, err := json.Marshal(share.GetOutbound().Config)
		if err != nil {
			t.Fatal(err)
		}

		if string(raw) != want {
			t.Errorf("%s:\n got %s\nwant %s", link, raw, want)
		}
	}

	// kcp sing-box не поддерживает.
	if _, err := ParseShareUrl("vmess://eyJwcyI6ImtjcCIsImFkZCI6ImsuZXhhbXBsZS5jb20iLCJwb3J0Ijo0NDMsImlkIjoidSIsIm5ldCI6ImtjcCJ9"); err == nil {
		t.Error("kcp must be rejected")
	}
}

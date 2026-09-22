package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

// makeKey упаковывает конфиг так же, как Amnezia: qCompress (длина + zlib) и base64url.
func makeKey(t *testing.T, config any) string {
	t.Helper()

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	var compressed bytes.Buffer

	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(raw)
	_ = writer.Close()

	data := binary.BigEndian.AppendUint32(nil, uint32(len(raw)))
	data = append(data, compressed.Bytes()...)

	return "vpn://" + base64.RawURLEncoding.EncodeToString(data)
}

// awgExport возвращает экспорт Amnezia с контейнером AmneziaWG 2.0 (AWG 3.1 параметры).
func awgExport(t *testing.T) map[string]any {
	t.Helper()

	lastConfig, _ := json.Marshal(map[string]any{
		"H1": "1", "H2": "2", "H3": "100-200", "H4": "4",
		"Jc": "4", "Jmin": "10", "Jmax": "50",
		"S1": "12", "S2": "12", "S3": "12", "S4": "12",
		"I1":                    "<r 2>",
		"HeaderProtectionKey":   "HPK",
		"RandomTrailers":        "on",
		"DisableCookies":        "off",
		"RekeyAfterTime":        "100-120",
		"FutureParam":           "x",
		"allowed_ips":           []string{"0.0.0.0/0"},
		"client_ip":             "10.8.1.5",
		"client_priv_key":       "PRIV",
		"server_pub_key":        "PUB",
		"psk_key":               "PSK",
		"hostName":              "203.0.113.10",
		"port":                  48026,
		"mtu":                   "1376",
		"persistent_keep_alive": "25-35",
	})

	return map[string]any{
		"description": "Сервер 1",
		"hostName":    "203.0.113.10",
		"containers": []any{
			map[string]any{
				"container": "amnezia-awg2",
				"awg": map[string]any{
					"I2":          "",
					"port":        "48026",
					"last_config": string(lastConfig),
				},
			},
			map[string]any{
				"container": "amnezia-openvpn",
			},
		},
	}
}

// TestParseAWG проверяет перенос параметров WireGuard и обфускации AmneziaWG.
func TestParseAWG(t *testing.T) {
	parsed, err := Parse(makeKey(t, awgExport(t)), "Home")
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(parsed.Items))
	}

	item := parsed.Items[0]
	config := item.Config

	if item.Tag != "[Home] AmneziaWG" || !item.RequiresAWG || config["type"] != "wireguard" {
		t.Errorf("unexpected item: %+v", item)
	}

	checks := map[string]any{
		"private_key":           "PRIV",
		"mtu":                   1376,
		"jc":                    4,
		"s3":                    12,
		"h1":                    1,
		"h3":                    "100-200",
		"i1":                    "<r 2>",
		"header_protection_key": "HPK",
		"random_trailers":       true,
		"disable_cookies":       false,
		"rekey_after_time":      "100-120",
	}

	for key, want := range checks {
		if config[key] != want {
			t.Errorf("%s = %#v, want %#v", key, config[key], want)
		}
	}

	if _, ok := config["i2"]; ok {
		t.Error("empty I2 must be omitted")
	}

	if address := config["address"].([]string); address[0] != "10.8.1.5/32" {
		t.Errorf("address = %v", address)
	}

	peer := config["peers"].([]any)[0].(map[string]any)

	if peer["address"] != "203.0.113.10" || peer["port"] != 48026 || peer["pre_shared_key"] != "PSK" || peer["persistent_keepalive_interval"] != "25-35" {
		t.Errorf("unexpected peer: %+v", peer)
	}

	// Неизвестный параметр AWG и неподдерживаемый контейнер попадают в предупреждения.
	if len(parsed.Warnings) != 2 {
		t.Errorf("warnings = %v, want 2", parsed.Warnings)
	}
}

// TestParseWireGuard проверяет, что обычный WireGuard не требует AmneziaWG.
func TestParseWireGuard(t *testing.T) {
	lastConfig, _ := json.Marshal(map[string]any{
		"client_ip":       "10.8.1.2/32",
		"client_priv_key": "PRIV",
		"server_pub_key":  "PUB",
		"hostName":        "203.0.113.10",
		"port":            "51820",
	})

	key := makeKey(t, map[string]any{
		"hostName": "203.0.113.10",
		"containers": []any{
			map[string]any{
				"container": "amnezia-wireguard",
				"wireguard": map[string]any{
					"last_config": string(lastConfig),
				},
			},
		},
	})

	parsed, err := Parse(key, "WG")
	if err != nil {
		t.Fatal(err)
	}

	item := parsed.Items[0]

	if item.RequiresAWG || item.Config["jc"] != nil {
		t.Errorf("plain wireguard must not require AWG: %+v", item.Config)
	}

	if _, ok := item.Config["peers"].([]any)[0].(map[string]any)["pre_shared_key"]; ok {
		t.Error("empty psk must be omitted")
	}
}

// TestParsePremium проверяет отказ для ключей Amnezia Premium.
func TestParsePremium(t *testing.T) {
	key := makeKey(t, map[string]any{
		"config_version": 2,
		"api_config":     map[string]any{"service_type": "amnezia-premium"},
		"auth_data":      map[string]any{"api_key": "KEY"},
	})

	if _, err := Parse(key, "P"); !errors.Is(err, ErrPremiumKey) {
		t.Errorf("err = %v, want ErrPremiumKey", err)
	}
}

// TestParseInvalid проверяет ошибки формата ключа.
func TestParseInvalid(t *testing.T) {
	for _, key := range []string{"", "vless://x", "vpn://!!!", "vpn://" + base64.RawURLEncoding.EncodeToString([]byte("junk"))} {
		if _, err := Parse(key, "X"); err == nil {
			t.Errorf("key %q must be rejected", key)
		}
	}
}

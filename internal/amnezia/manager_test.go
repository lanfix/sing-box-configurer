package amnezia

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/repository/appdata"
)

// outboundsByTag возвращает outbound-ы профилей для рендера, сгруппированные по тегу, и предупреждения.
func outboundsByTag(manager *Manager) (map[string]map[string]any, []string) {
	subscriptions, warnings := manager.Subscriptions()
	result := make(map[string]map[string]any)

	for _, subscription := range subscriptions {
		for _, outbound := range subscription.Outbounds {
			result[outbound["tag"].(string)] = outbound
		}
	}

	return result, warnings
}

// fakeVersion возвращает заданную версию sing-box.
type fakeVersion string

func (f fakeVersion) GetVersion() (string, error) {
	return string(f), nil
}

// fakeGateway — шлюз Amnezia со своей RSA-парой: расшифровывает запросы так же, как настоящий.
type fakeGateway struct {
	t          *testing.T
	privateKey *rsa.PrivateKey
	server     *httptest.Server

	mu       sync.Mutex
	requests map[string][]GatewayRequest
	failWith int
	rejected int
}

func newFakeGateway(t *testing.T) (*fakeGateway, *GatewayClient) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	gateway := &fakeGateway{
		t:          t,
		privateKey: privateKey,
		requests:   map[string][]GatewayRequest{},
	}

	gateway.server = httptest.NewServer(http.HandlerFunc(gateway.handle))
	t.Cleanup(gateway.server.Close)

	client, err := newGatewayClient(gateway.server.URL, publicKeyPEM(t, &privateKey.PublicKey))
	if err != nil {
		t.Fatal(err)
	}

	return gateway, client
}

// publicKeyPEM кодирует RSA-ключ в PEM.
func publicKeyPEM(t *testing.T, key *rsa.PublicKey) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// handle расшифровывает запрос, запоминает его и отвечает зашифрованной конфигурацией.
func (g *fakeGateway) handle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeyPayload string `json:"key_payload"`
		APIPayload string `json:"api_payload"`
	}

	raw, _ := io.ReadAll(r.Body)

	if err := json.Unmarshal(raw, &body); err != nil {
		g.t.Errorf("invalid request body: %v", err)

		return
	}

	encryptedKey, _ := base64.StdEncoding.DecodeString(body.KeyPayload)

	keyJSON, err := rsa.DecryptPKCS1v15(rand.Reader, g.privateKey, encryptedKey)
	if err != nil {
		// Так отвечает настоящий шлюз на запрос, зашифрованный чужим ключом.
		g.mu.Lock()
		g.rejected++
		g.mu.Unlock()

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	var keyPayload struct {
		Key string `json:"aes_key"`
		IV  string `json:"aes_iv"`
	}

	_ = json.Unmarshal(keyJSON, &keyPayload)

	key, _ := base64.StdEncoding.DecodeString(keyPayload.Key)
	iv, _ := base64.StdEncoding.DecodeString(keyPayload.IV)
	encryptedPayload, _ := base64.StdEncoding.DecodeString(body.APIPayload)

	payload, err := aesDecrypt(encryptedPayload, key, iv[:16])
	if err != nil {
		g.t.Errorf("cannot decrypt api payload: %v", err)

		return
	}

	var request GatewayRequest

	_ = json.Unmarshal(payload, &request)

	endpoint := strings.TrimPrefix(r.URL.Path, "/")

	g.mu.Lock()
	g.requests[endpoint] = append(g.requests[endpoint], request)
	failWith := g.failWith
	g.mu.Unlock()

	var response any = map[string]any{}

	if failWith != 0 {
		encrypted, _ := aesEncrypt([]byte(`{"detail":"subscription expired"}`), key, iv[:16])
		w.WriteHeader(failWith)
		_, _ = w.Write(encrypted)

		return
	}

	if endpoint == "v1/config" {
		response = map[string]any{
			"config": g.config(request),
		}
	}

	plain, _ := json.Marshal(response)
	encrypted, _ := aesEncrypt(plain, key, iv[:16])
	_, _ = w.Write(encrypted)
}

// config собирает конфигурацию подписки, как ее отдает шлюз: ключ vpn:// с плейсхолдером приватного ключа.
func (g *fakeGateway) config(request GatewayRequest) string {
	country := coalesce(request.ServerCountryCode, "ch")

	lastConfig, _ := json.Marshal(map[string]any{
		"Jc": "4", "Jmin": "10", "Jmax": "50", "S1": "12", "S2": "12", "H1": "1", "H2": "2", "H3": "3", "H4": "4",
		"client_ip":       "10.9.0.7",
		"client_priv_key": privateKeyPlaceholder,
		"client_pub_key":  request.PublicKey,
		"server_pub_key":  "SERVER",
		"hostName":        country + ".premium.example",
		"port":            "443",
	})

	exported := map[string]any{
		"config_version": 2,
		"description":    "Amnezia Premium",
		"hostName":       country + ".premium.example",
		"containers": []any{
			map[string]any{
				"container": "amnezia-awg",
				"awg": map[string]any{
					"last_config": string(lastConfig),
				},
			},
		},
		"api_config": map[string]any{
			"server_country_code": country,
			"available_countries": []any{
				map[string]any{"server_country_code": "ch", "server_country_name": "Switzerland"},
				map[string]any{"server_country_code": "nl", "server_country_name": "Netherlands"},
			},
			"subscription": map[string]any{"end_date": "2026-10-06 12:54:28+00:00"},
			"public_key":   map[string]any{"expires_at": "2026-11-01 00:00:00+00:00"},
		},
	}

	return makeKeyNoTest(exported)
}

// makeKeyNoTest упаковывает конфиг в ключ vpn:// без *testing.T (для обработчика сервера).
func makeKeyNoTest(config any) string {
	var t testing.T

	return makeKey(&t, config)
}

// premiumKey возвращает ключ подписки Amnezia Premium.
func premiumKey(t *testing.T) string {
	return makeKey(t, map[string]any{
		"config_version": 2,
		"name":           "Amnezia Premium",
		"api_config": map[string]any{
			"service_type":      "amnezia-premium",
			"service_protocol":  "vless",
			"user_country_code": "ru",
		},
		"auth_data": map[string]any{"api_key": "SECRET"},
	})
}

// TestManagerAWGSupport проверяет, что AWG-сервер не попадает в конфиг официального sing-box.
func TestManagerAWGSupport(t *testing.T) {
	appData := appdata.NewFile(filepath.Join(t.TempDir(), "app.json"))

	manager, err := NewManager(appData, fakeVersion("sing-box 1.14.0"), nil)
	if err != nil {
		t.Fatal(err)
	}

	profile, err := manager.Add(context.Background(), makeKey(t, awgExport(t)), "")
	if err != nil {
		t.Fatal(err)
	}

	if profile.Name != "Сервер 1" {
		t.Errorf("name = %q", profile.Name)
	}

	if outbounds, warnings := outboundsByTag(manager); len(outbounds) != 0 || len(warnings) != 1 {
		t.Errorf("AWG must be skipped without support: outbounds=%v warnings=%v", outbounds, warnings)
	}

	// Профиль сохранился и после перезагрузки менеджера попадает в конфиг форка sing-box-lx.
	manager, err = NewManager(appData, fakeVersion("sing-box 1.14.1-lx.8"), nil)
	if err != nil {
		t.Fatal(err)
	}

	if outbounds, _ := outboundsByTag(manager); outbounds["[Сервер 1] AmneziaWG"] == nil {
		t.Errorf("endpoint must be rendered: %v", outbounds)
	}

	if _, err = manager.Refresh(context.Background(), profile.ID); err == nil {
		t.Error("own server config must not be refreshable")
	}

	if _, err = manager.Delete(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}

	if outbounds, _ := outboundsByTag(manager); len(outbounds) != 0 || len(manager.List()) != 0 {
		t.Errorf("delete must remove endpoint and profile: %v", outbounds)
	}
}

// TestManagerPremium проверяет импорт подписки Amnezia Premium через шлюз.
func TestManagerPremium(t *testing.T) {
	gateway, client := newFakeGateway(t)
	appData := appdata.NewFile(filepath.Join(t.TempDir(), "app.json"))

	manager, err := NewManager(appData, fakeVersion("sing-box 1.14.1-lx.8"), client)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	profile, err := manager.Add(ctx, premiumKey(t), "")
	if err != nil {
		t.Fatal(err)
	}

	if profile.Name != "Amnezia Premium" || profile.Premium == nil {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	first := gateway.requests["v1/config"][0]

	if first.ServiceProtocol != "awg" || first.AuthData["api_key"] != "SECRET" || first.UserCountryCode != "ru" || first.InstallationUUID == "" {
		t.Errorf("unexpected gateway request: %+v", first)
	}

	publicKey, _ := wireGuardPublicKey(profile.Premium.WireGuardPrivateKey)

	if first.PublicKey != publicKey {
		t.Error("gateway must receive public key of the stored private key")
	}

	outbounds, _ := outboundsByTag(manager)

	endpoint := outbounds["[Amnezia Premium] AmneziaWG"]
	if endpoint == nil || endpoint["private_key"] != profile.Premium.WireGuardPrivateKey || endpoint["jc"] != 4 {
		t.Fatalf("private key placeholder must be replaced: %+v", endpoint)
	}

	premium := profile.Premium

	if premium.ServerCountryCode != "ch" || premium.ServerCountryName != "Switzerland" || len(premium.AvailableCountries) != 2 ||
		premium.SubscriptionEnd == "" || premium.ConfigExpiresAt == "" {
		t.Errorf("subscription info must be parsed: %+v", premium)
	}

	// Обновление использует те же ID инсталляции и ключ: шлюз не должен считать это новым устройством.
	if _, err = manager.Refresh(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}

	second := gateway.requests["v1/config"][1]

	if second.InstallationUUID != first.InstallationUUID || second.PublicKey != first.PublicKey {
		t.Error("refresh must reuse installation uuid and key")
	}

	if _, err = manager.SetCountry(ctx, profile.ID, "xx"); err == nil {
		t.Error("unknown country must be rejected")
	}

	updated, err := manager.SetCountry(ctx, profile.ID, "nl")
	if err != nil {
		t.Fatal(err)
	}

	if gateway.requests["v1/config"][2].ServerCountryCode != "nl" || updated.Server != "nl.premium.example" {
		t.Errorf("country must be requested and applied: %+v", updated)
	}

	// Ошибка шлюза сохраняется в профиле, прежние серверы остаются.
	gateway.failWith = http.StatusPaymentRequired

	if _, err = manager.Refresh(ctx, profile.ID); err == nil || !strings.Contains(err.Error(), "subscription expired") {
		t.Errorf("gateway error must be reported, got %v", err)
	}

	if stored := manager.List()[0]; stored.LastError == "" || len(stored.Items) != 1 {
		t.Errorf("error must be saved and items kept: %+v", stored)
	}

	gateway.failWith = 0

	if _, err = manager.Delete(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}

	revokes := gateway.requests["v1/revoke_config"]

	if len(revokes) != 1 || revokes[0].InstallationUUID != first.InstallationUUID {
		t.Errorf("delete must revoke the device: %+v", revokes)
	}

	if outbounds, _ = outboundsByTag(manager); len(outbounds) != 0 {
		t.Errorf("delete must remove endpoint: %v", outbounds)
	}
}

// TestGatewayCrypto проверяет AES-шифрование в обе стороны и ключи WireGuard.
func TestGatewayCrypto(t *testing.T) {
	key, iv := make([]byte, 32), make([]byte, 16)

	for _, data := range []string{"", "a", "exactly16bytes!!", `{"config":"vpn://..."}`} {
		encrypted, err := aesEncrypt([]byte(data), key, iv)
		if err != nil {
			t.Fatal(err)
		}

		decrypted, err := aesDecrypt(encrypted, key, iv)
		if err != nil || string(decrypted) != data {
			t.Errorf("roundtrip %q: got %q, %v", data, decrypted, err)
		}
	}

	privateKey, publicKey, err := generateWireGuardKeys()
	if err != nil {
		t.Fatal(err)
	}

	derived, err := wireGuardPublicKey(privateKey)
	if err != nil || derived != publicKey {
		t.Errorf("public key mismatch: %q vs %q (%v)", derived, publicKey, err)
	}
}

// TestGatewayKeyFallback проверяет переход на следующий ключ, если шлюз сменил ключ.
func TestGatewayKeyFallback(t *testing.T) {
	gateway, _ := newFakeGateway(t)

	staleKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	client, err := newGatewayClient(gateway.server.URL, publicKeyPEM(t, &staleKey.PublicKey), publicKeyPEM(t, &gateway.privateKey.PublicKey))
	if err != nil {
		t.Fatal(err)
	}

	state, _, err := decodePremiumKey(premiumKey(t))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if _, err = client.FetchConfig(context.Background(), state, "P"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	// Устаревший ключ пробуется только один раз, дальше клиент сразу использует рабочий.
	if gateway.rejected != 1 {
		t.Errorf("rejected = %d, want 1", gateway.rejected)
	}

	// Если не подошел ни один ключ, возвращается понятная ошибка.
	onlyStale, _ := newGatewayClient(gateway.server.URL, publicKeyPEM(t, &staleKey.PublicKey))

	if _, err = onlyStale.FetchConfig(context.Background(), state, "P"); !errors.Is(err, errKeyRejected) {
		t.Errorf("err = %v, want errKeyRejected", err)
	}
}

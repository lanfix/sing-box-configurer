package amnezia

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultGatewayURL — адрес шлюза Amnezia Premium.
	DefaultGatewayURL = "http://gw.amnezia.org:80/"

	// Параметры клиента, которым представляется сервис (как десктопное приложение Amnezia VPN).
	gatewayAppVersion = "5.0.3.0"
	gatewayCliName    = "AmneziaVPN"
	gatewayOSVersion  = "linux"

	gatewayTimeout = 20 * time.Second
)

// gatewayPublicKeysPEM — RSA-ключи шлюза из клиента Amnezia VPN, ими шифруется одноразовый ключ AES запроса.
// Шлюз периодически меняет ключ: клиент пробует ключи по порядку и запоминает подошедший.
// Первый — текущий ключ gw.amnezia.org, второй — прежний (сейчас принимается только dev-шлюзом).
var gatewayPublicKeysPEM = []string{
	`-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAj5mxl/4DL3Sk89ntxs5G
X3JawGQWIoq6rvNkOzNGuNgedNS2+pi6hZl3Izl1Io9om4KiUlMT6mgLO1hTr9q+
s7CYhlvroFA7ErucF+9L+7FCt0Igi0kIK/R2/vxd/2HaUrorn/aSvvutkYwbfxqW
SwtzE+RuBeDWGvEt937OW0oqYONPYv9E4T56Dz/EZ6v2t8ejAnKLbGD/GocMmipK
7etFSiSMAB2RmaztqTq4NleBepfO80XpYlW9pCSXuHcE8wxHczkzxsbyMAMsG/K3
vUQY6qPtohqqzSSBwa/8u2ptNHBeor7l7DdYXeR/Nqcc4z92VUkZ5lOVR4evkS5V
/wQqp5tnOJEj3NjUhEhXFoNEapbZd1bh6iQoUk7jC1TdvKJ/nPKGZAsHRpr0rNKz
fx/N/Oo6lr2yh/+ps6VxTkbPmB6E85WOO3UvjImZUY0XQdBjWle/4iJLdEC77Nr0
jXhdgeypucy6jkB6iBHMeVMlrNMEV7UxoBR/cCNx55zu/8sml5ByiDvCDT7sRomN
NgVt5S/FaVjYuzFUifJ12ToChXFgESKFmuso7WluEaWvMIGREdrMrKQKHfYLOzWF
2B5ZJDqw4o03fU4J/6rw61M1b+rjVpXMjPnzc2A+RgcjTvXv955gfZkwe4lt5wk/
3j8zMVo3+zLrMTAaEeIUM0UCAwEAAQ==
-----END PUBLIC KEY-----`,
	`-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAwMJbYlGxn3l+0XiGA9I/
BHK8HX/aet7A9GVL817apDUeL6sdISRBdopv5Y0FdrBHSJWSUdWtVxVazJB46J8x
327/5H5pi0nkfRbcgxBGSGxhKOvwRe+WPVb2f81jlkenZK46c9C7dNmX/310rlHY
BwOnZcdw2oKu6hTNDwk3nyUo2v2/leNIMLsv84RlHAX6Tyx5slq8ysewhcmdfv17
WQjF7albq12ZafTSjtXqDcsrk2oF8mfyzxLjSXbxQHKIDHkfz3SUXCs/H9tt1ydK
2Yj6nIxv98HESZ8Ng40OZPhHDex8Ru1NjcWlo2EWNM1xT8IqmBT21PLuyzGjNSwG
Ojnm1V2EcjerVmRNhFTJG70RkURD/i2MDbG+ZKpqPtW1uL8wEt2IkSqNfKcf+TF+
UJZZfm1lDUMpWJ2eWJGrgOUX8/f8v/GB+x4PxUo1m7V/pDLqCUPm3l2dkaM9P0sM
6lO0+jKqfIFnG1zjc3if7r1YbDsZlyl389q9Hrh7t+Lwj/JXkDxFaTnudM8egaXk
GX5YxZiEDmCCLRskRwBBUaYffXIpFbI8sO2Xj0J5/im5xtu7TtfJktcPzDL9uyG1
Ebt8oSA4FTzTid6Zwj55YgDfz0FMnNmXh80T1xMzlbi6y+BCuna+I+7McMRo8yz3
VzzYJ0/J7PpHpXoZv7K1qDsCAwEAAQ==
-----END PUBLIC KEY-----`,
}

// GatewayRequest — запрос к шлюзу (поля как в клиенте Amnezia VPN).
type GatewayRequest struct {
	OSVersion         string            `json:"os_version"`
	AppVersion        string            `json:"app_version"`
	CliName           string            `json:"cli_name"`
	AppLanguage       string            `json:"app_language"`
	InstallationUUID  string            `json:"installation_uuid"`
	UserCountryCode   string            `json:"user_country_code,omitempty"`
	ServerCountryCode string            `json:"server_country_code,omitempty"`
	ServiceType       string            `json:"service_type"`
	ServiceProtocol   string            `json:"service_protocol,omitempty"`
	PublicKey         string            `json:"public_key,omitempty"`
	AuthData          map[string]string `json:"auth_data"`
}

// errKeyRejected — шлюз не смог расшифровать запрос: он отвечает 500 с пустым телом, если ключ устарел.
var errKeyRejected = errors.New("шлюз не принял ключ шифрования (500 без ответа): вероятно, ключ шлюза сменился")

// GatewayClient выполняет зашифрованные запросы к шлюзу Amnezia.
type GatewayClient struct {
	baseURL    string
	publicKeys []*rsa.PublicKey
	httpClient *http.Client

	// active — индекс ключа, который шлюз принял последним.
	active atomic.Int32
}

// NewGatewayClient создает клиент шлюза. Пустой baseURL — адрес по умолчанию.
func NewGatewayClient(baseURL string) (*GatewayClient, error) {
	return newGatewayClient(baseURL, gatewayPublicKeysPEM...)
}

// newGatewayClient создает клиент с указанными ключами шлюза (используется в тестах).
func newGatewayClient(baseURL string, publicKeysPEM ...string) (*GatewayClient, error) {
	publicKeys := make([]*rsa.PublicKey, 0, len(publicKeysPEM))

	for _, publicKeyPEM := range publicKeysPEM {
		block, _ := pem.Decode([]byte(publicKeyPEM))
		if block == nil {
			return nil, fmt.Errorf("cannot parse gateway public key")
		}

		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("cannot parse gateway public key: %w", err)
		}

		publicKey, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("gateway public key is not RSA")
		}

		publicKeys = append(publicKeys, publicKey)
	}

	if len(publicKeys) == 0 {
		return nil, fmt.Errorf("no gateway public keys")
	}

	if baseURL == "" {
		baseURL = DefaultGatewayURL
	}

	return &GatewayClient{
		baseURL:    strings.TrimSuffix(baseURL, "/") + "/",
		publicKeys: publicKeys,
		httpClient: &http.Client{
			Timeout: gatewayTimeout,
		},
	}, nil
}

// Post отправляет запрос на endpoint (например "v1/config") и возвращает расшифрованное тело ответа.
// Если шлюз не принимает ключ шифрования, пробуются остальные известные ключи.
func (c *GatewayClient) Post(ctx context.Context, endpoint string, payload GatewayRequest) ([]byte, error) {
	start := int(c.active.Load())

	var err error

	for i := range c.publicKeys {
		index := (start + i) % len(c.publicKeys)

		var body []byte

		body, err = c.post(ctx, endpoint, payload, c.publicKeys[index])
		if errors.Is(err, errKeyRejected) {
			continue
		}

		// Шлюз расшифровал запрос (даже если ответил ошибкой) — ключ рабочий.
		c.active.Store(int32(index))

		return body, err
	}

	return nil, err
}

// post выполняет запрос, шифруя одноразовый ключ AES ключом шлюза publicKey.
func (c *GatewayClient) post(ctx context.Context, endpoint string, payload GatewayRequest, publicKey *rsa.PublicKey) ([]byte, error) {
	// Одноразовый ключ AES: шлюз шифрует им ответ. IV генерируется на 32 байта,
	// но AES-256-CBC использует первые 16 — как в клиенте Amnezia (OpenSSL).
	key, iv, salt := make([]byte, 32), make([]byte, 32), make([]byte, 8)

	for _, buf := range [][]byte{key, iv, salt} {
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("cannot generate aes key: %w", err)
		}
	}

	keyPayload, err := json.Marshal(map[string]string{
		"aes_key":  base64.StdEncoding.EncodeToString(key),
		"aes_iv":   base64.StdEncoding.EncodeToString(iv),
		"aes_salt": base64.StdEncoding.EncodeToString(salt),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot marshal key payload: %w", err)
	}

	encryptedKey, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, keyPayload)
	if err != nil {
		return nil, fmt.Errorf("cannot encrypt key payload: %w", err)
	}

	apiPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal api payload: %w", err)
	}

	encryptedPayload, err := aesEncrypt(apiPayload, key, iv[:aes.BlockSize])
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]string{
		"key_payload": base64.StdEncoding.EncodeToString(encryptedKey),
		"api_payload": base64.StdEncoding.EncodeToString(encryptedPayload),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Request-ID", uuid.NewString())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("шлюз Amnezia недоступен: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("cannot read response: %w", err)
	}

	if resp.StatusCode == http.StatusInternalServerError && len(bytes.TrimSpace(respBody)) == 0 {
		return nil, errKeyRejected
	}

	// Ответ (в том числе с ошибкой) обычно зашифрован; если расшифровать не удалось — это текст ошибки.
	decrypted, decryptErr := aesDecrypt(respBody, key, iv[:aes.BlockSize])

	if resp.StatusCode != http.StatusOK {
		message := strings.TrimSpace(string(respBody))

		if decryptErr == nil {
			message = strings.TrimSpace(string(decrypted))
		}

		return nil, gatewayError(resp.StatusCode, message)
	}

	if decryptErr != nil {
		return nil, fmt.Errorf("cannot decrypt gateway response: %w", decryptErr)
	}

	return decrypted, nil
}

// gatewayError формирует понятную ошибку по статусу ответа шлюза.
func gatewayError(status int, message string) error {
	var parsed struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	if json.Unmarshal([]byte(message), &parsed) == nil {
		message = coalesce(parsed.Detail, parsed.Message, parsed.Error, message)
	}

	if len(message) > 300 {
		message = message[:300] + "..."
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("шлюз отклонил ключ (%d): %s", status, message)

	case http.StatusPaymentRequired:
		return fmt.Errorf("подписка Amnezia Premium истекла (%d): %s", status, message)

	case http.StatusNotFound:
		return fmt.Errorf("шлюз не нашел конфигурацию (%d): %s", status, message)

	case http.StatusConflict:
		return fmt.Errorf("в подписке закончились места для устройств — удалите лишнее устройство в приложении Amnezia VPN и повторите (%d): %s", status, message)
	}

	return fmt.Errorf("шлюз вернул ошибку %d: %s", status, message)
}

// aesEncrypt шифрует данные AES-256-CBC с PKCS#7.
func aesEncrypt(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot create cipher: %w", err)
	}

	padding := aes.BlockSize - len(data)%aes.BlockSize
	padded := append(bytes.Clone(data), bytes.Repeat([]byte{byte(padding)}, padding)...)

	result := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(result, padded)

	return result, nil
}

// aesDecrypt расшифровывает данные AES-256-CBC с PKCS#7.
func aesDecrypt(data, key, iv []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid ciphertext size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot create cipher: %w", err)
	}

	result := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(result, data)

	padding := int(result[len(result)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(result) {
		return nil, errors.New("invalid padding")
	}

	for _, b := range result[len(result)-padding:] {
		if int(b) != padding {
			return nil, errors.New("invalid padding")
		}
	}

	return result[:len(result)-padding], nil
}

// generateWireGuardKeys генерирует пару ключей WireGuard (X25519) в base64.
func generateWireGuardKeys() (string, string, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("cannot generate wireguard key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(privateKey.Bytes()),
		base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

// wireGuardPublicKey вычисляет публичный ключ WireGuard по приватному.
func wireGuardPublicKey(privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("invalid wireguard private key: %w", err)
	}

	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("invalid wireguard private key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

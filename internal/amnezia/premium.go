package amnezia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	// premiumProtocol — протокол, который запрашивается у шлюза. VLESS шлюз отдает с транспортом XHTTP,
	// который sing-box не поддерживает, поэтому используется AmneziaWG.
	premiumProtocol = "awg"

	// privateKeyPlaceholder — место приватного ключа клиента в конфигурации от шлюза.
	privateKeyPlaceholder = "$WIREGUARD_CLIENT_PRIVATE_KEY"

	premiumLanguage = "ru"
)

// Country — страна сервера Amnezia Premium.
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// PremiumState — данные подписки Amnezia Premium. Ключ WireGuard и ID инсталляции постоянны для профиля:
// шлюз учитывает устройства по installation_uuid, и повторные запросы не занимают новые места.
type PremiumState struct {
	APIKey              string    `json:"api_key"`
	ServiceType         string    `json:"service_type"`
	UserCountryCode     string    `json:"user_country_code"`
	ServerCountryCode   string    `json:"server_country_code"`
	ServerCountryName   string    `json:"server_country_name"`
	InstallationUUID    string    `json:"installation_uuid"`
	WireGuardPrivateKey string    `json:"wireguard_private_key"`
	AvailableCountries  []Country `json:"available_countries"`
	SubscriptionEnd     string    `json:"subscription_end"`
	ConfigExpiresAt     string    `json:"config_expires_at"`
}

// premiumAPIConfig — нужная часть api_config из ключа и из ответа шлюза.
type premiumAPIConfig struct {
	ServiceType        string `json:"service_type"`
	UserCountryCode    string `json:"user_country_code"`
	ServerCountryCode  string `json:"server_country_code"`
	ServerCountryName  string `json:"server_country_name"`
	AvailableCountries []struct {
		Code string `json:"server_country_code"`
		Name string `json:"server_country_name"`
	} `json:"available_countries"`
	Subscription struct {
		EndDate string `json:"end_date"`
	} `json:"subscription"`
	PublicKey struct {
		ExpiresAt string `json:"expires_at"`
	} `json:"public_key"`
}

// decodePremiumKey возвращает состояние новой подписки, если ключ — Amnezia Premium, и nil для обычного ключа.
func decodePremiumKey(key string) (*PremiumState, *export, error) {
	raw, err := decodeKey(key)
	if err != nil {
		return nil, nil, err
	}

	var data export

	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, nil, fmt.Errorf("cannot parse config: %w", err)
	}

	if !data.isPremiumKey() {
		return nil, &data, nil
	}

	var apiConfig premiumAPIConfig

	if len(data.APIConfig) > 0 {
		if err = json.Unmarshal(data.APIConfig, &apiConfig); err != nil {
			return nil, nil, fmt.Errorf("cannot parse api_config: %w", err)
		}
	}

	privateKey, _, err := generateWireGuardKeys()
	if err != nil {
		return nil, nil, err
	}

	state := &PremiumState{
		APIKey:              data.AuthData.APIKey,
		ServiceType:         coalesce(apiConfig.ServiceType, "amnezia-premium"),
		UserCountryCode:     coalesce(apiConfig.UserCountryCode, "ru"),
		ServerCountryCode:   apiConfig.ServerCountryCode,
		ServerCountryName:   "",
		InstallationUUID:    uuid.NewString(),
		WireGuardPrivateKey: privateKey,
		AvailableCountries:  []Country{},
		SubscriptionEnd:     "",
		ConfigExpiresAt:     "",
	}

	return state, &data, nil
}

// FetchConfig запрашивает у шлюза конфигурацию AmneziaWG и обновляет сведения о подписке в state.
func (c *GatewayClient) FetchConfig(ctx context.Context, state *PremiumState, profileName string) (*Parsed, error) {
	publicKey, err := wireGuardPublicKey(state.WireGuardPrivateKey)
	if err != nil {
		return nil, err
	}

	body, err := c.Post(ctx, "v1/config", c.request(state, premiumProtocol, publicKey))
	if err != nil {
		return nil, err
	}

	var response struct {
		Config string `json:"config"`
	}

	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("cannot parse gateway response: %w", err)
	}

	if response.Config == "" {
		return nil, fmt.Errorf("шлюз вернул пустую конфигурацию")
	}

	raw, err := decodePayload(response.Config)
	if err != nil {
		return nil, fmt.Errorf("cannot decode gateway config: %w", err)
	}

	raw = bytes.ReplaceAll(raw, []byte(privateKeyPlaceholder), []byte(state.WireGuardPrivateKey))

	parsed, err := parseExport(raw, profileName)
	if err != nil {
		return nil, err
	}

	state.applyAPIConfig(raw)

	return parsed, nil
}

// RevokeConfig освобождает место устройства в подписке.
func (c *GatewayClient) RevokeConfig(ctx context.Context, state *PremiumState) error {
	_, err := c.Post(ctx, "v1/revoke_config", c.request(state, "", ""))

	return err
}

// request собирает запрос к шлюзу для подписки.
func (c *GatewayClient) request(state *PremiumState, protocol, publicKey string) GatewayRequest {
	return GatewayRequest{
		OSVersion:         gatewayOSVersion,
		AppVersion:        gatewayAppVersion,
		CliName:           gatewayCliName,
		AppLanguage:       premiumLanguage,
		InstallationUUID:  state.InstallationUUID,
		UserCountryCode:   state.UserCountryCode,
		ServerCountryCode: state.ServerCountryCode,
		ServiceType:       state.ServiceType,
		ServiceProtocol:   protocol,
		PublicKey:         publicKey,
		AuthData: map[string]string{
			"api_key": state.APIKey,
		},
	}
}

// applyAPIConfig переносит сведения о подписке из конфигурации шлюза.
func (s *PremiumState) applyAPIConfig(raw []byte) {
	var data struct {
		APIConfig premiumAPIConfig `json:"api_config"`
	}

	if json.Unmarshal(raw, &data) != nil {
		return
	}

	apiConfig := data.APIConfig

	if apiConfig.ServerCountryCode != "" {
		s.ServerCountryCode = apiConfig.ServerCountryCode
	}

	s.SubscriptionEnd = apiConfig.Subscription.EndDate
	s.ConfigExpiresAt = apiConfig.PublicKey.ExpiresAt

	// Список стран обновляем до вычисления названия текущей страны.
	if len(apiConfig.AvailableCountries) > 0 {
		s.AvailableCountries = s.AvailableCountries[:0]

		for _, country := range apiConfig.AvailableCountries {
			s.AvailableCountries = append(s.AvailableCountries, Country{
				Code: country.Code,
				Name: coalesce(country.Name, strings.ToUpper(country.Code)),
			})
		}
	}

	s.ServerCountryName = coalesce(apiConfig.ServerCountryName, s.countryName(s.ServerCountryCode))
}

// countryName возвращает название страны по коду из списка доступных.
func (s *PremiumState) countryName(code string) string {
	for _, country := range s.AvailableCountries {
		if country.Code == code {
			return country.Name
		}
	}

	return strings.ToUpper(code)
}

// hasCountry проверяет, что страна есть в списке доступных.
func (s *PremiumState) hasCountry(code string) bool {
	for _, country := range s.AvailableCountries {
		if country.Code == code {
			return true
		}
	}

	return false
}

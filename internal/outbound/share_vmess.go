package outbound

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const VMessProtocolName = "vmess"

// vmessSecurities — методы шифрования VMess в sing-box.
var vmessSecurities = []string{"auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305", "aes-128-ctr"}

type ShareVMess struct {
	outbound Outbound
}

func (s *ShareVMess) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareVMessProvider struct{}

func NewShareVMessProvider() *ShareVMessProvider {
	return &ShareVMessProvider{}
}

// vmessLink — ссылка VMess в формате v2rayN: JSON в base64. Числа встречаются и строками, и числами.
type vmessLink struct {
	Name          string     `json:"ps"`
	Address       string     `json:"add"`
	Port          flexString `json:"port"`
	ID            string     `json:"id"`
	AlterID       flexString `json:"aid"`
	Security      string     `json:"scy"`
	Network       string     `json:"net"`
	HeaderType    string     `json:"type"`
	Host          string     `json:"host"`
	Path          string     `json:"path"`
	TLS           string     `json:"tls"`
	SNI           string     `json:"sni"`
	ALPN          string     `json:"alpn"`
	Fingerprint   string     `json:"fp"`
	AllowInsecure flexString `json:"allowInsecure"`
}

// flexString — поле JSON, которое приходит строкой или числом.
type flexString string

// UnmarshalJSON принимает строку, число или булево значение.
func (f *flexString) UnmarshalJSON(data []byte) error {
	var text string

	if err := json.Unmarshal(data, &text); err == nil {
		*f = flexString(text)

		return nil
	}

	var value any

	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	if value == nil {
		*f = ""

		return nil
	}

	*f = flexString(fmt.Sprint(value))

	return nil
}

// Parse разбирает ссылку vmess:// в формате v2rayN (base64 от JSON) или в формате
// vmess://uuid@host:port?type=ws&security=tls#имя.
func (p *ShareVMessProvider) Parse(shareData string) (Share, error) {
	body, _, _ := strings.Cut(shareData, "#")

	if decoded, err := decodeBase64(body); err == nil && strings.HasPrefix(strings.TrimSpace(decoded), "{") {
		var link vmessLink

		if err = json.Unmarshal([]byte(decoded), &link); err != nil {
			return nil, fmt.Errorf("некорректный JSON ссылки: %w", err)
		}

		return vmessFromLink(link)
	}

	return vmessFromURI(shareData)
}

// vmessFromLink собирает outbound из ссылки формата v2rayN.
func vmessFromLink(link vmessLink) (Share, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(string(link.Port)), 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("invalid port number")
	}

	alterID := 0

	if raw := strings.TrimSpace(string(link.AlterID)); raw != "" {
		if alterID, err = strconv.Atoi(raw); err != nil {
			return nil, fmt.Errorf("некорректный alterId %q", raw)
		}
	}

	if link.HeaderType != "" && link.HeaderType != "none" && (link.Network == "" || link.Network == "tcp") {
		return nil, fmt.Errorf("TCP с обфускацией %s не поддерживается sing-box", link.HeaderType)
	}

	params := url.Values{}
	params.Set("type", link.Network)
	params.Set("path", link.Path)
	params.Set("host", link.Host)

	// В v2rayN имя gRPC-сервиса передается в path.
	if link.Network == "grpc" {
		params.Set("serviceName", link.Path)
	}

	tls := map[string]any(nil)

	if link.TLS == "tls" {
		tls = vmessTLS(coalesce(link.SNI, link.Host), link.ALPN, link.Fingerprint, string(link.AllowInsecure))
	}

	return newVMessShare(vmessParams{
		name:     unescapeProfileName(link.Name),
		host:     strings.TrimSpace(link.Address),
		port:     uint16(port),
		uuid:     strings.TrimSpace(link.ID),
		alterID:  alterID,
		security: link.Security,
		params:   params,
		tls:      tls,
	})
}

// vmessFromURI собирает outbound из ссылки вида vmess://uuid@host:port?параметры#имя.
func vmessFromURI(shareData string) (Share, error) {
	common, err := extractCommonShareDataFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("ссылка не в формате v2rayN (base64) и не в формате uuid@host:port: %w", err)
	}

	if common.port == nil || *common.port == 0 || common.portRange != nil {
		return nil, fmt.Errorf("invalid port number")
	}

	params := common.params
	if params == nil {
		params = url.Values{}
	}

	alterID := 0

	if raw := params.Get("alterId"); raw != "" {
		if alterID, err = strconv.Atoi(raw); err != nil {
			return nil, fmt.Errorf("некорректный alterId %q", raw)
		}
	}

	tls := map[string]any(nil)

	if security := params.Get("security"); security == "tls" {
		tls = vmessTLS(coalesce(params.Get("sni"), params.Get("host")), params.Get("alpn"), params.Get("fp"), params.Get("allowInsecure"))
	}

	return newVMessShare(vmessParams{
		name:     common.profileName,
		host:     common.host,
		port:     *common.port,
		uuid:     common.credentials,
		alterID:  alterID,
		security: params.Get("encryption"),
		params:   params,
		tls:      tls,
	})
}

// vmessParams — разобранные параметры ссылки VMess.
type vmessParams struct {
	name     string
	host     string
	port     uint16
	uuid     string
	alterID  int
	security string
	params   url.Values
	tls      map[string]any
}

// newVMessShare собирает outbound VMess sing-box.
func newVMessShare(p vmessParams) (Share, error) {
	if p.host == "" {
		return nil, fmt.Errorf("host is required")
	}

	if p.uuid == "" {
		return nil, fmt.Errorf("uuid is required")
	}

	security := strings.ToLower(coalesce(p.security, "auto"))

	if !slices.Contains(vmessSecurities, security) {
		return nil, fmt.Errorf("метод шифрования VMess %q не поддерживается sing-box", security)
	}

	profileName := coalesce(p.name, fmt.Sprintf("%s-%s", VMessProtocolName, p.host))

	config := map[string]any{
		"type":        VMessProtocolName,
		"tag":         profileName,
		"server":      p.host,
		"server_port": p.port,
		"uuid":        p.uuid,
		"security":    security,
		"alter_id":    p.alterID,
	}

	if p.tls != nil {
		config["tls"] = p.tls
	}

	if p.params.Get("type") == "quic" {
		config["transport"] = map[string]any{
			"type": "quic",
		}
	} else {
		transport, err := shareTransport(p.params)
		if err != nil {
			return nil, err
		}

		if transport != nil {
			config["transport"] = transport
		}
	}

	return &ShareVMess{
		outbound: Outbound{
			Tag:        profileName,
			Type:       VMessProtocolName,
			Server:     p.host,
			Port:       int(p.port),
			Config:     config,
			IsEndpoint: false,
		},
	}, nil
}

// vmessTLS возвращает настройки TLS ссылки VMess.
func vmessTLS(serverName, alpn, fingerprint, insecure string) map[string]any {
	tls := map[string]any{
		"enabled": true,
	}

	if serverName != "" {
		tls["server_name"] = serverName
	}

	if alpn != "" {
		values := make([]any, 0)

		for _, item := range strings.Split(alpn, ",") {
			if item = strings.TrimSpace(item); item != "" {
				values = append(values, item)
			}
		}

		tls["alpn"] = values
	}

	if fingerprint != "" {
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
	}

	if insecure == "1" || strings.EqualFold(insecure, "true") {
		tls["insecure"] = true
	}

	return tls
}

package outbound

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Share interface {
	GetOutbound() *Outbound
}

type ShareProvider interface {
	Parse(shareData string) (Share, error)
}

// Outbound представляет VPN outbound конфигурацию.
type Outbound struct {
	Tag        string         `json:"tag"`
	Type       string         `json:"type"`
	Server     string         `json:"server"`
	Port       int            `json:"server_port"`
	Config     map[string]any `json:"-"`
	IsEndpoint bool           `json:"is_endpoint"`
}

func ParseShareUrl(shareUrl string) (Share, error) {
	shareUrl = strings.TrimSpace(shareUrl)

	if shareUrl == "" {
		return nil, fmt.Errorf("share url is empty")
	}

	if !strings.Contains(shareUrl, "://") {
		return nil, fmt.Errorf("share url must contain protocol name and ://")
	}

	parts := strings.Split(shareUrl, "://")

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid share url")
	}

	protocol := parts[0]
	shareData := parts[1]

	var shareProvider ShareProvider

	switch protocol {
	case "vless":
		shareProvider = NewShareVLessProvider()

	case "hysteria2", "hy2":
		shareProvider = NewShareHysteria2Provider()

	case "wireguard", "wg":
		shareProvider = NewShareWireGuardProvider()

	case "trojan":
		shareProvider = NewShareTrojanProvider()

	case "ss":
		shareProvider = NewShareShadowsocksProvider()

	case "vmess":
		shareProvider = NewShareVMessProvider()

	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}

	share, err := shareProvider.Parse(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot parse share data: %s", err)
	}

	return share, nil
}

func extractProfileNameFromShare(shareData string) (newShareData, profileName string, err error) {
	parts := strings.Split(shareData, "#")

	switch len(parts) {
	case 1:
		return parts[0], "", nil

	case 2:
		return parts[0], unescapeProfileName(parts[1]), nil

	default:
		return "", "", fmt.Errorf("invalid share data format, there are many # symbols")
	}
}

func extractCredentialsFromShare(shareData string) (newShareData, credentials string, err error) {
	parts := strings.Split(shareData, "@")

	switch len(parts) {
	case 1:
		return parts[0], "", nil

	case 2:
		return parts[1], parts[0], nil

	default:
		return "", "", fmt.Errorf("invalid share data format, there are many @ symbols")
	}
}

func extractParamsFromShare(shareData string) (newShareData string, params url.Values, err error) {
	parts := strings.Split(shareData, "?")

	switch len(parts) {
	case 1:
		return parts[0], nil, nil

	case 2:
		params, err = url.ParseQuery(parts[1])
		if err != nil {
			return "", nil, fmt.Errorf("cannot parse share data params: %s", err)
		}

		return parts[0], params, nil

	default:
		return "", nil, fmt.Errorf("invalid share data format, there are many ? symbols")
	}
}

type PortRange struct {
	Min uint16
	Max uint16
}

func extractHostPortFromShare(shareData string) (host string, port *uint16, portRange *PortRange, err error) {
	parts := strings.Split(shareData, ":")

	switch len(parts) {
	case 1:
		return parts[0], nil, nil, nil

	case 2:
		portParts := strings.Split(parts[1], "-")
		switch len(portParts) {
		case 1:
			portTmp, err := strconv.ParseUint(portParts[0], 10, 16)
			if err != nil {
				return "", nil, nil, fmt.Errorf("cannot parse share data port: %s", err)
			}

			return parts[0], new(uint16(portTmp)), nil, nil

		case 2:
			portMin, err := strconv.ParseUint(portParts[0], 10, 16)
			if err != nil {
				return "", nil, nil, fmt.Errorf("cannot parse share data min port: %s", err)
			}

			portMax, err := strconv.ParseUint(portParts[1], 10, 16)
			if err != nil {
				return "", nil, nil, fmt.Errorf("cannot parse share data max port: %s", err)
			}

			portRange = &PortRange{
				Min: uint16(portMin),
				Max: uint16(portMax),
			}

			return parts[0], nil, portRange, nil

		default:
			return "", nil, nil, fmt.Errorf("invalid share data, there are many - symbols in port section")
		}

	default:
		return "", nil, nil, fmt.Errorf("invalid share data format, there are many : symbols")
	}
}

type CommonShareData struct {
	host        string
	port        *uint16
	portRange   *PortRange
	params      url.Values
	profileName string
	credentials string
}

func extractCommonShareDataFromShare(shareData string) (*CommonShareData, error) {
	shareData, profileName, err := extractProfileNameFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract profile name from share: %w", err)
	}

	shareData, credentials, err := extractCredentialsFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract credentials from share: %w", err)
	}

	shareData, params, err := extractParamsFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract params from share: %w", err)
	}

	host, port, portRange, err := extractHostPortFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract host and port from share: %w", err)
	}

	return &CommonShareData{
		host:        host,
		port:        port,
		portRange:   portRange,
		params:      params,
		profileName: profileName,
		credentials: credentials,
	}, nil
}

func coalesce[T comparable](val ...T) T {
	var zero T

	for i := range val {
		if val[i] != zero {
			return val[i]
		}
	}

	return zero
}

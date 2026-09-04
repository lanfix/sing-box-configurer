package outbound

import (
	"fmt"
)

const VLessProtocolName = "vless"

type ShareVLess struct {
	outbound Outbound
}

func (s *ShareVLess) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareVLessProvider struct{}

func NewShareVLessProvider() *ShareVLessProvider {
	return &ShareVLessProvider{}
}

func (p *ShareVLessProvider) Parse(shareData string) (Share, error) {
	shareCommonData, err := extractCommonShareDataFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract share data: %s", err)
	}

	if shareCommonData.host == "" {
		return nil, fmt.Errorf("host is required")
	}

	if shareCommonData.port == nil || *shareCommonData.port == 0 {
		return nil, fmt.Errorf("invalid port number")
	}

	if shareCommonData.portRange != nil {
		return nil, fmt.Errorf("invalid port, range is not allowed for %s protocol", VLessProtocolName)
	}

	// TODO: Такое имя уже может быть в конфиге.
	profileName := coalesce(shareCommonData.profileName, fmt.Sprintf("%s-%s", VLessProtocolName, shareCommonData.host))

	config := map[string]any{
		"type":        VLessProtocolName,
		"tag":         profileName,
		"server":      shareCommonData.host,
		"server_port": *shareCommonData.port,
		"uuid":        shareCommonData.credentials,
	}

	security := shareCommonData.params.Get("security")
	if security == "tls" {
		tlsConfig := map[string]any{
			"enabled": true,
		}

		if sni := shareCommonData.params.Get("sni"); sni != "" {
			tlsConfig["server_name"] = sni
		}

		if fp := shareCommonData.params.Get("fp"); fp != "" {
			tlsConfig["utls"] = map[string]any{
				"enabled":     true,
				"fingerprint": fp,
			}
		}

		config["tls"] = tlsConfig
	}

	switch coalesce(shareCommonData.params.Get("type"), "tcp") {
	case "tcp":
		// TCP не требует дополнительной конфигурации.

	case "ws":
		wsConfig := map[string]any{}

		if path := shareCommonData.params.Get("path"); path != "" {
			wsConfig["path"] = path
		}

		if host := shareCommonData.params.Get("host"); host != "" {
			wsConfig["headers"] = map[string]any{
				"Host": host,
			}
		}

		config["transport"] = map[string]any{
			"type": "ws",
			"ws":   wsConfig,
		}

	case "xhttp":
		xhttpConfig := map[string]any{}

		if path := shareCommonData.params.Get("path"); path != "" {
			xhttpConfig["path"] = path
		}

		if host := shareCommonData.params.Get("host"); host != "" {
			xhttpConfig["host"] = host
		}

		if mode := shareCommonData.params.Get("mode"); mode != "" {
			xhttpConfig["mode"] = mode
		}

		config["transport"] = map[string]any{
			"type":  "httpupgrade",
			"xhttp": xhttpConfig,
		}

	case "grpc":
		grpcConfig := map[string]any{}

		if serviceName := shareCommonData.params.Get("serviceName"); serviceName != "" {
			grpcConfig["service_name"] = serviceName
		}

		config["transport"] = map[string]any{
			"type": "grpc",
			"grpc": grpcConfig,
		}
	}

	if encryption := shareCommonData.params.Get("encryption"); encryption != "" && encryption != "none" {
		config["encryption"] = encryption
	}

	return &ShareVLess{
		outbound: Outbound{
			Tag:        profileName,
			Type:       VLessProtocolName,
			Server:     shareCommonData.host,
			Port:       int(*shareCommonData.port),
			Config:     config,
			IsEndpoint: false,
		},
	}, nil
}

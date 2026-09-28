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

	// Уникальность тега обеспечивает менеджер outbound-ов при добавлении.
	profileName := coalesce(shareCommonData.profileName, fmt.Sprintf("%s-%s", VLessProtocolName, shareCommonData.host))

	config := map[string]any{
		"type":        VLessProtocolName,
		"tag":         profileName,
		"server":      shareCommonData.host,
		"server_port": *shareCommonData.port,
		"uuid":        shareCommonData.credentials,
	}

	if flow := shareCommonData.params.Get("flow"); flow != "" {
		config["flow"] = flow
	}

	security := shareCommonData.params.Get("security")
	if security == "tls" || security == "reality" {
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

		if security == "reality" {
			publicKey := shareCommonData.params.Get("pbk")
			if publicKey == "" {
				return nil, fmt.Errorf("public key (pbk) is required for reality")
			}

			tlsConfig["reality"] = map[string]any{
				"enabled":    true,
				"public_key": publicKey,
				"short_id":   shareCommonData.params.Get("sid"),
			}

			// Reality в sing-box работает только через uTLS.
			if _, ok := tlsConfig["utls"]; !ok {
				tlsConfig["utls"] = map[string]any{
					"enabled":     true,
					"fingerprint": "chrome",
				}
			}
		}

		config["tls"] = tlsConfig
	}

	transport, err := shareTransport(shareCommonData.params)
	if err != nil {
		return nil, err
	}

	if transport != nil {
		config["transport"] = transport
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

package outbound

import (
	"fmt"
)

const TrojanProtocolName = "trojan"

type ShareTrojan struct {
	outbound Outbound
}

func (s *ShareTrojan) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareTrojanProvider struct{}

func NewShareTrojanProvider() *ShareTrojanProvider {
	return &ShareTrojanProvider{}
}

func (p *ShareTrojanProvider) Parse(shareData string) (Share, error) {
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
		return nil, fmt.Errorf("invalid port, range is not allowed for %s protocol", TrojanProtocolName)
	}

	if shareCommonData.credentials == "" {
		return nil, fmt.Errorf("password is required")
	}

	profileName := coalesce(shareCommonData.profileName, fmt.Sprintf("%s-%s", TrojanProtocolName, shareCommonData.host))

	config := map[string]any{
		"type":        TrojanProtocolName,
		"tag":         profileName,
		"server":      shareCommonData.host,
		"server_port": *shareCommonData.port,
		"password":    shareCommonData.credentials,
	}

	security := shareCommonData.params.Get("security")
	if security == "" || security == "tls" {
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

	transport, err := shareTransport(shareCommonData.params)
	if err != nil {
		return nil, err
	}

	if transport != nil {
		config["transport"] = transport
	}

	return &ShareTrojan{
		outbound: Outbound{
			Tag:        profileName,
			Type:       TrojanProtocolName,
			Server:     shareCommonData.host,
			Port:       int(*shareCommonData.port),
			Config:     config,
			IsEndpoint: false,
		},
	}, nil
}

package outbound

import (
	"fmt"
)

const Hysteria2ProtocolName = "hysteria2"

type ShareHysteria2 struct {
	outbound Outbound
}

func (s *ShareHysteria2) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareHysteria2Provider struct{}

func NewShareHysteria2Provider() *ShareHysteria2Provider {
	return &ShareHysteria2Provider{}
}

func (p *ShareHysteria2Provider) Parse(shareData string) (Share, error) {
	shareCommonData, err := extractCommonShareDataFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract share data: %s", err)
	}

	if shareCommonData.host == "" {
		return nil, fmt.Errorf("host is required")
	}

	if (shareCommonData.port == nil || *shareCommonData.port == 0) && shareCommonData.portRange == nil {
		return nil, fmt.Errorf("port number or port range is required")
	}

	if shareCommonData.port != nil && *shareCommonData.port != 0 && shareCommonData.portRange != nil {
		return nil, fmt.Errorf("only port or port range can be specified")
	}

	// TODO: Такое имя уже может быть в конфиге.
	profileName := coalesce(shareCommonData.profileName, fmt.Sprintf("%s-%s", Hysteria2ProtocolName, shareCommonData.host))

	config := map[string]interface{}{
		"type":     "hysteria2",
		"tag":      profileName,
		"server":   shareCommonData.host,
		"password": shareCommonData.credentials,
	}

	var port int

	if shareCommonData.port != nil {
		port = int(*shareCommonData.port)
		config["server_port"] = *shareCommonData.port
	}

	if shareCommonData.portRange != nil {
		port = int(shareCommonData.portRange.Min)
		portRange := fmt.Sprintf("%d-%d", shareCommonData.portRange.Min, shareCommonData.portRange.Max)
		config["server_ports"] = []any{portRange}
	}

	if sni := shareCommonData.params.Get("sni"); sni != "" {
		config["tls"] = map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
		}
	}

	obfs := shareCommonData.params.Get("obfs")
	if obfs != "" {
		obfsPassword := shareCommonData.params.Get("obfs-password")
		if obfsPassword == "" {
			obfsPassword = shareCommonData.params.Get("obfs-pass")
		}

		if obfs == "salamander" && obfsPassword != "" {
			config["obfs"] = map[string]interface{}{
				"type":     "salamander",
				"password": obfsPassword,
			}
		}
	}

	return &ShareHysteria2{
		outbound: Outbound{
			Tag:        profileName,
			Type:       Hysteria2ProtocolName,
			Server:     shareCommonData.host,
			Port:       port,
			Config:     config,
			IsEndpoint: false,
		},
	}, nil
}

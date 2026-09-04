package outbound

import (
	"fmt"
	"strconv"
	"strings"
)

const WireGuardProtocolName = "wireguard"

type ShareWireGuard struct {
	outbound Outbound
}

func (s *ShareWireGuard) GetOutbound() *Outbound {
	return &s.outbound
}

type ShareWireGuardProvider struct{}

func NewShareWireGuardProvider() *ShareWireGuardProvider {
	return &ShareWireGuardProvider{}
}

func (p *ShareWireGuardProvider) Parse(shareData string) (Share, error) {
	shareCommonData, err := extractCommonShareDataFromShare(shareData)
	if err != nil {
		return nil, fmt.Errorf("cannot extract share data: %s", err)
	}

	if shareCommonData.credentials == "" {
		return nil, fmt.Errorf("private key is required")
	}

	publicKey := shareCommonData.params.Get("public_key")
	if publicKey == "" {
		publicKey = shareCommonData.params.Get("publickey")
	}

	if publicKey == "" {
		return nil, fmt.Errorf("public key is required")
	}

	server := shareCommonData.params.Get("endpoint")
	if server == "" {
		server = shareCommonData.host
	}

	if server == "" {
		return nil, fmt.Errorf("server endpoint is required")
	}

	var port uint16 = 51820

	if shareCommonData.port != nil {
		port = *shareCommonData.port
	} else if portStr := shareCommonData.params.Get("port"); portStr != "" {
		portVal, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("cannot parse port: %w", err)
		}

		port = uint16(portVal)
	}

	if shareCommonData.portRange != nil {
		return nil, fmt.Errorf("invalid port, range is not allowed for %s protocol", WireGuardProtocolName)
	}

	profileName := coalesce(shareCommonData.profileName, fmt.Sprintf("%s-%s", WireGuardProtocolName, strings.Split(server, ":")[0]))

	address := shareCommonData.params.Get("address")
	if address == "" {
		address = shareCommonData.params.Get("addresses")
	}

	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	allowedIPs := "0.0.0.0/0"

	if allowed := shareCommonData.params.Get("allowed_ips"); allowed != "" {
		allowedIPs = allowed
	}

	allowedIPsList := strings.Split(allowedIPs, ",")

	peer := map[string]any{
		"address":     server,
		"port":        port,
		"public_key":  publicKey,
		"allowed_ips": allowedIPsList,
	}

	if preSharedKey := shareCommonData.params.Get("pre_shared_key"); preSharedKey != "" {
		peer["pre_shared_key"] = preSharedKey
	}

	if psk := shareCommonData.params.Get("preshared_key"); psk != "" {
		peer["pre_shared_key"] = psk
	}

	if keepalive := shareCommonData.params.Get("persistent_keepalive"); keepalive != "" {
		if ka, err := strconv.Atoi(keepalive); err == nil {
			peer["persistent_keepalive_interval"] = ka
		}
	}

	config := map[string]any{
		"type":        WireGuardProtocolName,
		"tag":         profileName,
		"private_key": shareCommonData.credentials,
		"address":     address,
		"peers": []any{
			peer,
		},
	}

	if mtu := shareCommonData.params.Get("mtu"); mtu != "" {
		if mtuVal, err := strconv.Atoi(mtu); err == nil {
			config["mtu"] = mtuVal
		}
	}

	return &ShareWireGuard{
		outbound: Outbound{
			Tag:        profileName,
			Type:       WireGuardProtocolName,
			Server:     server,
			Port:       int(port),
			Config:     config,
			IsEndpoint: true,
		},
	}, nil
}

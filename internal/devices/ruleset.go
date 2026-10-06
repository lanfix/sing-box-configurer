package devices

import (
	"github.com/lanfix/sing-box-configurer/internal/rules"
)

// networks возвращает сети LAN, к которым применяется профиль по умолчанию.
func (d Data) networks() []string {
	result := make([]string, 0, len(d.Settings.Networks)+len(d.Detected.Networks))

	if d.Settings.AutoNetworks {
		for _, network := range d.Detected.Networks {
			result = appendUnique(result, network)
		}
	}

	for _, network := range d.Settings.Networks {
		result = appendUnique(result, network)
	}

	return result
}

// exclusions возвращает адреса, к которым профиль по умолчанию не применяется: адреса самого хоста
// (его трафик идет без MAC) и исключения из настроек.
func (d Data) exclusions() []string {
	result := make([]string, 0, len(d.Detected.HostAddresses)+len(d.Settings.Exclude))

	for _, address := range d.Detected.HostAddresses {
		result = appendUnique(result, address)
	}

	for _, address := range d.Settings.Exclude {
		result = appendUnique(result, address)
	}

	return result
}

// macs возвращает MAC-адреса устройств с профилем profile.
func (d Data) macs(profile string) []string {
	result := make([]string, 0)

	for _, device := range d.Devices {
		if device.Profile == profile {
			result = append(result, device.MAC)
		}
	}

	return result
}

// explicitMACs возвращает MAC-адреса устройств со своим профилем: на них профиль по умолчанию не действует.
func (d Data) explicitMACs() []string {
	result := make([]string, 0, len(d.Devices))

	for _, device := range d.Devices {
		if device.Profile != ProfileDefault {
			result = append(result, device.MAC)
		}
	}

	return result
}

// unknownRule возвращает правило для устройств без своего профиля: источник в сетях LAN, не адрес хоста
// и не исключение, MAC не из списка. Без сетей LAN правила нет.
func (d Data) unknownRule() map[string]any {
	networks := d.networks()
	if len(networks) == 0 {
		return nil
	}

	conditions := []any{
		map[string]any{
			"source_ip_cidr": networks,
		},
	}

	if exclusions := d.exclusions(); len(exclusions) > 0 {
		conditions = append(conditions, map[string]any{
			"source_ip_cidr": exclusions,
			"invert":         true,
		})
	}

	if known := d.explicitMACs(); len(known) > 0 {
		conditions = append(conditions, map[string]any{
			"source_mac_address": known,
			"invert":             true,
		})
	}

	if len(conditions) == 1 {
		return conditions[0].(map[string]any)
	}

	return map[string]any{
		"type":  "logical",
		"mode":  "and",
		"rules": conditions,
	}
}

// ruleSet возвращает rule-set профиля: устройства с этим профилем и, если он профиль по умолчанию,
// неизвестные устройства.
func (d Data) ruleSet(profile string) rules.SingBoxRuleSet {
	result := rules.SingBoxRuleSet{
		Version: rules.RuleVersion,
		Rules:   []map[string]interface{}{},
	}

	if macs := d.macs(profile); len(macs) > 0 {
		result.Rules = append(result.Rules, map[string]any{
			"source_mac_address": macs,
		})
	}

	if d.Settings.DefaultProfile != profile {
		return result
	}

	if rule := d.unknownRule(); rule != nil {
		result.Rules = append(result.Rules, rule)
	}

	return result
}

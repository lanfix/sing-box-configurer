package legacy

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// HostsServerTag — тег системного DNS-сервера с DNS-записями, заданными в конфигураторе.
const HostsServerTag = RuleSetTagPrefix + "-hosts"

// DNSRecord описывает DNS-запись: домен и адреса, которые вернет DNS sing-box.
type DNSRecord struct {
	Domain    string
	Addresses []string
}

// SyncDNSRecords приводит системный hosts-сервер и DNS-правило для его доменов к записям records.
// Правило ставится первым в dns.rules, чтобы записи имели приоритет над остальными правилами.
// Без записей системный сервер и правило удаляются. Функция идемпотентна.
func SyncDNSRecords(config map[string]any, records []DNSRecord) error {
	if _, ok := config["dns"]; !ok && len(records) == 0 {
		return nil
	}

	dns, err := getOrCreateMap(config, "dns")
	if err != nil {
		return err
	}

	servers, err := getSlice(dns, "servers")
	if err != nil {
		return fmt.Errorf(".dns: %w", err)
	}

	rules, err := getSlice(dns, "rules")
	if err != nil {
		return fmt.Errorf(".dns: %w", err)
	}

	servers = slices.DeleteFunc(servers, IsHostsServer)
	rules = slices.DeleteFunc(rules, IsHostsRule)

	if len(records) > 0 {
		server, rule := newHostsEntries(records)

		servers = append(servers, server)
		rules = slices.Insert(rules, 0, any(rule))
	}

	dns["servers"] = servers
	dns["rules"] = rules

	return nil
}

// CheckDNSRecordsSync проверяет, что системные hosts-сервер и правило в конфиге соответствуют записям records.
func CheckDNSRecordsSync(config map[string]any, records []DNSRecord) bool {
	dns, _ := config["dns"].(map[string]any)
	servers, _ := dns["servers"].([]any)
	rules, _ := dns["rules"].([]any)

	actualServers := slices.DeleteFunc(slices.Clone(servers), func(item any) bool {
		return !IsHostsServer(item)
	})

	actualRules := slices.DeleteFunc(slices.Clone(rules), func(item any) bool {
		return !IsHostsRule(item)
	})

	if len(records) == 0 {
		return len(actualServers) == 0 && len(actualRules) == 0
	}

	// Правило записей должно стоять первым.
	if len(actualServers) != 1 || len(actualRules) != 1 || !IsHostsRule(rules[0]) {
		return false
	}

	server, rule := newHostsEntries(records)

	return jsonEqual(actualServers[0], server) && jsonEqual(actualRules[0], rule)
}

// ImportHostsServers переносит в записи конфигуратора пользовательские hosts-серверы с predefined-записями.
// Сервер переносится, только если на него ссылаются лишь простые правила вида {"domain": [...], "server": ...}
// и у него нет файлов hosts (path). Такие серверы и правила удаляются из конфига.
// Возвращает перенесенные записи в порядке доменов.
func ImportHostsServers(config map[string]any) ([]DNSRecord, error) {
	dns, ok := config["dns"].(map[string]any)
	if !ok {
		return nil, nil
	}

	servers, err := getSlice(dns, "servers")
	if err != nil {
		return nil, fmt.Errorf(".dns: %w", err)
	}

	rules, err := getSlice(dns, "rules")
	if err != nil {
		return nil, fmt.Errorf(".dns: %w", err)
	}

	importable := map[string]map[string]any{}

	for _, item := range servers {
		server, ok := item.(map[string]any)
		if !ok || IsHostsServer(item) {
			continue
		}

		serverType, _ := extractFiledFromMapAny(server, "type")
		tag, _ := extractFiledFromMapAny(server, "tag")
		predefined, _ := server["predefined"].(map[string]any)

		if _, hasPath := server["path"]; serverType != "hosts" || tag == "" || hasPath || len(predefined) == 0 {
			continue
		}

		importable[tag] = predefined
	}

	// Сервер, на который ссылается не простое правило, оставляем как есть.
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}

		server, _ := extractFiledFromMapAny(rule, "server")

		if _, ok = importable[server]; ok && !isSimpleDomainRule(rule) {
			delete(importable, server)
		}
	}

	if len(importable) == 0 {
		return nil, nil
	}

	addresses := map[string][]string{}

	for _, predefined := range importable {
		for domain, value := range predefined {
			addresses[domain] = append(addresses[domain], AnyToStrings(value)...)
		}
	}

	records := make([]DNSRecord, 0, len(addresses))

	for domain, list := range addresses {
		slices.Sort(list)

		records = append(records, DNSRecord{
			Domain:    domain,
			Addresses: slices.Compact(list),
		})
	}

	slices.SortFunc(records, func(a, b DNSRecord) int {
		return strings.Compare(a.Domain, b.Domain)
	})

	dns["servers"] = slices.DeleteFunc(servers, func(item any) bool {
		server, _ := item.(map[string]any)
		tag, _ := extractFiledFromMapAny(server, "tag")
		_, ok := importable[tag]

		return ok
	})

	dns["rules"] = slices.DeleteFunc(rules, func(item any) bool {
		rule, _ := item.(map[string]any)
		server, _ := extractFiledFromMapAny(rule, "server")
		_, ok := importable[server]

		return ok
	})

	return records, nil
}

// newHostsEntries возвращает системный hosts-сервер и DNS-правило для записей records.
func newHostsEntries(records []DNSRecord) (map[string]any, map[string]any) {
	predefined := map[string]any{}
	domains := make([]any, 0, len(records))

	for _, record := range records {
		addresses := make([]any, 0, len(record.Addresses))

		for _, address := range record.Addresses {
			addresses = append(addresses, address)
		}

		predefined[record.Domain] = addresses
		domains = append(domains, record.Domain)
	}

	server := map[string]any{
		"predefined": predefined,
		"tag":        HostsServerTag,
		"type":       "hosts",
	}

	rule := map[string]any{
		"domain": domains,
		"server": HostsServerTag,
	}

	return server, rule
}

// IsHostsServer проверяет, что элемент — системный hosts-сервер.
func IsHostsServer(item any) bool {
	server, ok := item.(map[string]any)
	if !ok {
		return false
	}

	tag, _ := extractFiledFromMapAny(server, "tag")

	return tag == HostsServerTag
}

// IsHostsRule проверяет, что элемент — системное DNS-правило записей.
func IsHostsRule(item any) bool {
	rule, ok := item.(map[string]any)
	if !ok {
		return false
	}

	server, _ := extractFiledFromMapAny(rule, "server")

	return server == HostsServerTag
}

// isSimpleDomainRule проверяет, что правило направляет список доменов на сервер без других условий.
func isSimpleDomainRule(rule map[string]any) bool {
	if _, ok := rule["domain"]; !ok {
		return false
	}

	for key, value := range rule {
		switch key {
		case "domain", "server":
			continue

		case "action":
			if value != "route" {
				return false
			}

		default:
			return false
		}
	}

	return true
}

// AnyToStrings приводит строку или список строк из JSON к []string.
func AnyToStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}

	case []any:
		result := make([]string, 0, len(typed))

		for _, item := range typed {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}

		return result

	default:
		return nil
	}
}

// jsonEqual сравнивает значения по их JSON-представлению.
func jsonEqual(a, b any) bool {
	rawA, errA := json.Marshal(a)
	rawB, errB := json.Marshal(b)

	return errA == nil && errB == nil && string(rawA) == string(rawB)
}

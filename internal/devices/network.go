package devices

import (
	"bufio"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// virtualInterfaceRe — интерфейсы, за которыми нет устройств LAN: docker, туннели, VPN, loopback.
// Мосты docker называются br-<12 hex>, поэтому br-lan роутеров OpenWrt не исключается.
var virtualInterfaceRe = regexp.MustCompile(`^(lo|docker\d+|br-[0-9a-f]{12}|veth.*|tun\d*|tap\d*|wg\d*|tailscale\d*|zt.*|virbr\d+.*|cni\d*|flannel.*|cali.*|vxlan.*|kube.*)$`)

// Neighbor — запись таблицы соседей хоста с MAC-адресом.
type Neighbor struct {
	IP        netip.Addr
	MAC       string
	Interface string
}

// InterfaceAddress — адрес интерфейса хоста с префиксом сети.
type InterfaceAddress struct {
	Interface string
	Prefix    netip.Prefix
}

// HostNetwork — снимок сети хоста: адреса интерфейсов и соседи.
type HostNetwork struct {
	Addresses []InterfaceAddress
	Neighbors []Neighbor
}

// ParseAddresses разбирает вывод ip -o addr show (iproute2 и busybox).
func ParseAddresses(output string) []InterfaceAddress {
	result := make([]InterfaceAddress, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		// Формат: "2: eth0    inet 192.168.1.2/24 brd ... scope global eth0".
		if len(fields) < 4 || (fields[2] != "inet" && fields[2] != "inet6") {
			continue
		}

		prefix, err := netip.ParsePrefix(fields[3])
		if err != nil {
			continue
		}

		name, _, _ := strings.Cut(fields[1], "@")

		result = append(result, InterfaceAddress{
			Interface: name,
			Prefix:    prefix,
		})
	}

	return result
}

// ParseNeighbors разбирает вывод ip neigh show. Записи без MAC (FAILED, INCOMPLETE) пропускаются.
func ParseNeighbors(output string) []Neighbor {
	result := make([]Neighbor, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		if len(fields) == 0 {
			continue
		}

		ip, err := netip.ParseAddr(fields[0])
		if err != nil {
			continue
		}

		neighbor := Neighbor{
			IP:        ip.Unmap(),
			MAC:       "",
			Interface: "",
		}

		for i := 1; i+1 < len(fields); i++ {
			switch fields[i] {
			case "dev":
				neighbor.Interface = fields[i+1]

			case "lladdr":
				neighbor.MAC = NormalizeMAC(fields[i+1])
			}
		}

		if neighbor.MAC == "" || neighbor.Interface == "" {
			continue
		}

		result = append(result, neighbor)
	}

	return result
}

// NormalizeMAC приводит MAC-адрес к виду aa:bb:cc:dd:ee:ff. Для некорректного адреса возвращает пустую строку.
func NormalizeMAC(value string) string {
	mac, err := net.ParseMAC(strings.TrimSpace(value))
	if err != nil || len(mac) != 6 {
		return ""
	}

	return mac.String()
}

// isVirtualInterface проверяет, что за интерфейсом нет устройств LAN.
func isVirtualInterface(name string) bool {
	return virtualInterfaceRe.MatchString(name)
}

// lanNeighbors возвращает соседей на интерфейсах LAN.
func (n HostNetwork) lanNeighbors() []Neighbor {
	result := make([]Neighbor, 0, len(n.Neighbors))

	for _, neighbor := range n.Neighbors {
		if !isVirtualInterface(neighbor.Interface) {
			result = append(result, neighbor)
		}
	}

	return result
}

// Detect возвращает сети LAN (подсети интерфейсов, где есть соседи) и адреса самого хоста.
func (n HostNetwork) Detect() ([]string, []string) {
	interfaces := map[string]bool{}

	for _, neighbor := range n.lanNeighbors() {
		interfaces[neighbor.Interface] = true
	}

	networks := make([]string, 0)
	hostAddresses := make([]string, 0)

	for _, address := range n.Addresses {
		addr := address.Prefix.Addr()

		if addr.IsLoopback() || addr.IsLinkLocalUnicast() {
			continue
		}

		hostAddresses = appendUnique(hostAddresses, netip.PrefixFrom(addr, addr.BitLen()).String())

		if !interfaces[address.Interface] {
			continue
		}

		networks = appendUnique(networks, address.Prefix.Masked().String())
	}

	slices.Sort(networks)
	slices.Sort(hostAddresses)

	return networks, hostAddresses
}

// appendUnique добавляет value в список, если его там еще нет.
func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}

	return append(values, value)
}

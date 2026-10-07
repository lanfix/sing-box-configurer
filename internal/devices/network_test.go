package devices

import (
	"slices"
	"testing"
)

// iproute2Addresses — вывод ip -o addr show роутера с LAN-мостом, WAN, docker и tun sing-box.
const iproute2Addresses = `1: lo    inet 127.0.0.1/8 scope host lo\       valid_lft forever preferred_lft forever
1: lo    inet6 ::1/128 scope host noprefixroute \       valid_lft forever preferred_lft forever
2: br-lan    inet 192.168.50.6/24 brd 192.168.50.255 scope global br-lan\       valid_lft forever preferred_lft forever
2: br-lan    inet6 fd00:50::6/64 scope global \       valid_lft forever preferred_lft forever
2: br-lan    inet6 fe80::1/64 scope link \       valid_lft forever preferred_lft forever
3: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\       valid_lft forever preferred_lft forever
4: br-0123456789ab    inet 172.18.0.1/16 brd 172.18.255.255 scope global br-0123456789ab\       valid_lft forever preferred_lft forever
5: tun0    inet 198.18.0.1/30 brd 198.18.0.3 scope global tun0\       valid_lft forever preferred_lft forever
6: veth1a2b@if5    inet6 fe80::2/64 scope link \       valid_lft forever preferred_lft forever`

// busyboxNeighbors — вывод ip neigh show из busybox (контейнер sing-box).
const busyboxNeighbors = `192.168.50.10 dev br-lan lladdr AA:BB:CC:DD:EE:01 ref 1 used 0/0/0 probes 1 REACHABLE
192.168.50.11 dev br-lan lladdr aa:bb:cc:dd:ee:02 used 0/0/0 probes 1 STALE
192.168.50.12 dev br-lan  used 0/0/0 probes 6 FAILED
172.18.0.2 dev br-0123456789ab lladdr 02:42:ac:12:00:02 used 0/0/0 probes 0 STALE
fd00:50::10 dev br-lan lladdr aa:bb:cc:dd:ee:01 router used 0/0/0 probes 0 STALE`

func TestParseAddresses(t *testing.T) {
	addresses := ParseAddresses(iproute2Addresses)

	if len(addresses) != 9 {
		t.Fatalf("addresses = %d, want 9: %+v", len(addresses), addresses)
	}

	last := addresses[len(addresses)-1]

	if last.Interface != "veth1a2b" || last.Prefix.String() != "fe80::2/64" {
		t.Errorf("last address = %+v", last)
	}
}

func TestParseNeighbors(t *testing.T) {
	neighbors := ParseNeighbors(busyboxNeighbors)

	if len(neighbors) != 4 {
		t.Fatalf("neighbors = %d, want 4 (FAILED skipped): %+v", len(neighbors), neighbors)
	}

	if neighbors[0].MAC != "aa:bb:cc:dd:ee:01" || neighbors[0].Interface != "br-lan" || neighbors[0].IP.String() != "192.168.50.10" {
		t.Errorf("first neighbor = %+v", neighbors[0])
	}
}

func TestDetect(t *testing.T) {
	network := HostNetwork{
		Addresses: ParseAddresses(iproute2Addresses),
		Neighbors: ParseNeighbors(busyboxNeighbors),
	}

	networks, hostAddresses := network.Detect()

	// Docker-мост с соседом-контейнером не считается LAN, link-local адреса не попадают в сети.
	if want := []string{"192.168.50.0/24", "fd00:50::/64"}; !slices.Equal(networks, want) {
		t.Errorf("networks = %v, want %v", networks, want)
	}

	for _, address := range []string{"192.168.50.6/32", "fd00:50::6/128", "172.18.0.1/32", "198.18.0.1/32"} {
		if !slices.Contains(hostAddresses, address) {
			t.Errorf("host addresses %v have no %s", hostAddresses, address)
		}
	}

	if slices.Contains(hostAddresses, "127.0.0.1/32") {
		t.Errorf("host addresses %v contain loopback", hostAddresses)
	}

	if len(network.lanNeighbors()) != 3 {
		t.Errorf("lan neighbors = %+v, want 3 without docker container", network.lanNeighbors())
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"AA:BB:CC:DD:EE:FF":       "aa:bb:cc:dd:ee:ff",
		"aa-bb-cc-dd-ee-ff":       "aa:bb:cc:dd:ee:ff",
		" aabb.ccdd.eeff ":        "aa:bb:cc:dd:ee:ff",
		"aa:bb:cc":                "",
		"00:00:5e:00:53:01:02:03": "",
	}

	for input, want := range cases {
		if got := NormalizeMAC(input); got != want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseRoutesAndGateway(t *testing.T) {
	routes := ParseRoutes("default via 172.17.0.1 dev docker0\ndefault via 192.168.50.1 dev br-lan proto static metric 100\n10.0.0.0/8 dev wg0 scope link\n")

	if len(routes) != 2 || routes[1].Gateway.String() != "192.168.50.1" || routes[1].Interface != "br-lan" {
		t.Fatalf("routes = %+v", routes)
	}

	network := HostNetwork{
		Addresses: nil,
		Neighbors: nil,
		Routes:    routes,
	}

	// Шлюз docker пропускается, роутер — шлюз в сети LAN.
	if gateway := network.Gateway([]string{"192.168.50.0/24"}); gateway != "192.168.50.1" {
		t.Errorf("gateway = %q", gateway)
	}

	// Шлюз вне сетей LAN (например, провайдера на WAN) — не роутер сети.
	if gateway := network.Gateway([]string{"10.10.0.0/16"}); gateway != "" {
		t.Errorf("gateway outside LAN = %q", gateway)
	}
}

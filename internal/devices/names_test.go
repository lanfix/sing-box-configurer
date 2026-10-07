package devices

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

// ptrResponse собирает ответ на запрос query с PTR-записью name (имя ответа — ссылка на вопрос).
func ptrResponse(query []byte, name string) []byte {
	response := append([]byte{}, query...)
	response[2] |= 0x80
	binary.BigEndian.PutUint16(response[6:], 1)

	rdata := make([]byte, 0, len(name)+2)

	for _, label := range splitLabels(name) {
		rdata = append(rdata, byte(len(label)))
		rdata = append(rdata, label...)
	}

	rdata = append(rdata, 0)

	response = append(response, 0xc0, 12)
	response = binary.BigEndian.AppendUint16(response, dnsTypePTR)
	response = binary.BigEndian.AppendUint16(response, dnsClassIN)
	response = binary.BigEndian.AppendUint32(response, 60)
	response = binary.BigEndian.AppendUint16(response, uint16(len(rdata)))

	return append(response, rdata...)
}

// splitLabels разбивает имя на метки.
func splitLabels(name string) []string {
	labels := make([]string, 0)
	start := 0

	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			labels = append(labels, name[start:i])
			start = i + 1
		}
	}

	return labels
}

func TestReverseName(t *testing.T) {
	if got := reverseName(netip.MustParseAddr("192.168.50.20")); got != "20.50.168.192.in-addr.arpa" {
		t.Errorf("IPv4 reverse name = %s", got)
	}

	if got := reverseName(netip.MustParseAddr("fd00::1")); got != "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.d.f.ip6.arpa" {
		t.Errorf("IPv6 reverse name = %s", got)
	}
}

func TestQueryPTR(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = conn.Close()
	}()

	// Сервер отвечает именем с доменом роутера.
	go func() {
		buffer := make([]byte, 512)

		n, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}

		_, _ = conn.WriteTo(ptrResponse(buffer[:n], "Ivans-iPhone.lan"), addr)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server := netip.MustParseAddrPort(conn.LocalAddr().String())

	name, err := queryPTR(ctx, server, netip.MustParseAddr("192.168.50.20"), true)
	if err != nil || name != "Ivans-iPhone.lan" {
		t.Fatalf("queryPTR = %q, %v", name, err)
	}

	// Ответ на чужой запрос отклоняется.
	query := buildQuery(1, reverseName(netip.MustParseAddr("192.168.50.20")), dnsTypePTR, true)

	if _, err = parsePTRResponse(ptrResponse(query, "x.lan"), 2); err == nil {
		t.Error("response with another id accepted")
	}
}

func TestParseNBStat(t *testing.T) {
	response := []byte{0x12, 0x34, 0x84, 0x00, 0, 0, 0, 1, 0, 0, 0, 0}
	response = append(response, 0x20)

	for range 16 {
		response = append(response, 'C', 'K')
	}

	response = append(response, 0)
	response = binary.BigEndian.AppendUint16(response, dnsTypeNBStat)
	response = binary.BigEndian.AppendUint16(response, dnsClassIN)
	response = binary.BigEndian.AppendUint32(response, 0)
	response = binary.BigEndian.AppendUint16(response, 1+3*18)
	response = append(response, 3)

	// Рабочая группа (групповое имя), служба файлов (0x20) и имя компьютера.
	for _, entry := range []struct {
		name   string
		suffix byte
		flags  uint16
	}{
		{"WORKGROUP", 0x00, 0x8400},
		{"DESKTOP-7Q2", 0x20, 0x0400},
		{"DESKTOP-7Q2", 0x00, 0x0400},
	} {
		raw := []byte(entry.name + "               ")[:15]
		response = append(response, raw...)
		response = append(response, entry.suffix)
		response = binary.BigEndian.AppendUint16(response, entry.flags)
	}

	if name, err := parseNBStatResponse(response, 0x1234); err != nil || name != "DESKTOP-7Q2" {
		t.Errorf("parseNBStatResponse = %q, %v", name, err)
	}
}

func TestNormalizeHostname(t *testing.T) {
	ip := netip.MustParseAddr("192.168.50.20")

	cases := map[string]string{
		"Ivans-iPhone.local.": "Ivans-iPhone",
		"android-1a2b.lan":    "android-1a2b",
		"DESKTOP-7Q2":         "DESKTOP-7Q2",
		"192-168-50-20.lan":   "",
		"host-192-168-50-20":  "",
		"localhost":           "",
		"":                    "",
	}

	for name, want := range cases {
		if got := normalizeHostname(name, ip); got != want {
			t.Errorf("normalizeHostname(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestVendor(t *testing.T) {
	if vendor := Vendor("F0-D1-A9-00-00-01"); vendor != "Apple" {
		t.Errorf("Vendor(Apple) = %q", vendor)
	}

	if vendor := Vendor("bc:38:98:79:4b:9a"); vendor != "Intel" {
		t.Errorf("Vendor(Intel) = %q", vendor)
	}

	// Случайный адрес Wi-Fi телефона производителя не называет.
	if Vendor("f2:e7:36:28:18:1a") != "" || !IsRandomMAC("f2:e7:36:28:18:1a") || IsRandomMAC("f0:d1:a9:00:00:01") {
		t.Error("random MAC is not detected")
	}
}

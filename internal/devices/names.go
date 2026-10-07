package devices

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Источники имени устройства в порядке приоритета.
const (
	// NameSourceRouter — обратный DNS роутера: имя, которое устройство сообщило DHCP.
	NameSourceRouter = "router"

	// NameSourceMDNS — ответ самого устройства по mDNS (Apple, Linux, принтеры).
	NameSourceMDNS = "mdns"

	// NameSourceNetBIOS — имя компьютера по NetBIOS (Windows, Samba).
	NameSourceNetBIOS = "netbios"
)

const (
	// nameTimeout — сколько ждать ответа одного источника имени.
	nameTimeout = 1500 * time.Millisecond

	// maxHostnameLength ограничивает длину найденного имени.
	maxHostnameLength = 63

	// Типы и классы записей DNS и NetBIOS.
	dnsTypePTR    = 12
	dnsTypeNBStat = 33
	dnsClassIN    = 1
)

// NameResolver узнает имя устройства с адресом ip. gateway — адрес роутера, может быть пустым.
type NameResolver interface {
	Resolve(ctx context.Context, ip, gateway netip.Addr) (name string, source string)
}

// NetNameResolver опрашивает роутер (PTR) и само устройство (mDNS, NetBIOS) параллельно.
type NetNameResolver struct{}

// Resolve возвращает имя из самого приоритетного источника, который ответил.
func (NetNameResolver) Resolve(ctx context.Context, ip, gateway netip.Addr) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, nameTimeout)
	defer cancel()

	sources := []string{NameSourceRouter, NameSourceMDNS, NameSourceNetBIOS}
	names := make([]string, len(sources))

	var wg sync.WaitGroup

	for i, source := range sources {
		wg.Add(1)

		go func() {
			defer wg.Done()

			var (
				name string
				err  error
			)

			switch source {
			case NameSourceRouter:
				if gateway.IsValid() {
					name, err = queryPTR(ctx, netip.AddrPortFrom(gateway, 53), ip, true)
				}

			case NameSourceMDNS:
				name, err = queryPTR(ctx, netip.AddrPortFrom(ip, 5353), ip, false)

			case NameSourceNetBIOS:
				if ip.Is4() {
					name, err = queryNetBIOS(ctx, ip)
				}
			}

			if err == nil {
				names[i] = normalizeHostname(name, ip)
			}
		}()
	}

	wg.Wait()

	for i, name := range names {
		if name != "" {
			return name, sources[i]
		}
	}

	return "", ""
}

// queryPTR запрашивает у server PTR-запись адреса ip. recursion — флаг RD (для mDNS не ставится).
func queryPTR(ctx context.Context, server netip.AddrPort, ip netip.Addr, recursion bool) (string, error) {
	id := randomID()
	query := buildQuery(id, reverseName(ip), dnsTypePTR, recursion)

	response, err := exchangeUDP(ctx, server, query)
	if err != nil {
		return "", err
	}

	return parsePTRResponse(response, id)
}

// queryNetBIOS запрашивает у устройства таблицу имен NetBIOS (NBSTAT) и возвращает имя компьютера.
func queryNetBIOS(ctx context.Context, ip netip.Addr) (string, error) {
	id := randomID()

	// Имя "*" в кодировке NetBIOS: 16 байт (звездочка и нули), каждый полубайт — буква от 'A'.
	raw := make([]byte, 16)
	raw[0] = '*'
	name := make([]byte, 0, 32)

	for _, b := range raw {
		name = append(name, 'A'+b>>4, 'A'+b&0x0f)
	}

	query := buildQuery(id, string(name), dnsTypeNBStat, false)

	response, err := exchangeUDP(ctx, netip.AddrPortFrom(ip, 137), query)
	if err != nil {
		return "", err
	}

	return parseNBStatResponse(response, id)
}

// exchangeUDP отправляет запрос и ждет ответ до отмены ctx.
func exchangeUDP(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "udp", server.String())
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = conn.Close()
	}()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err = conn.Write(query); err != nil {
		return nil, err
	}

	buffer := make([]byte, 1500)

	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}

	return buffer[:n], nil
}

// buildQuery собирает DNS-запрос с одним вопросом name типа qtype.
func buildQuery(id uint16, name string, qtype uint16, recursion bool) []byte {
	query := make([]byte, 12, 12+len(name)+6)

	binary.BigEndian.PutUint16(query[0:], id)

	if recursion {
		query[2] = 0x01
	}

	binary.BigEndian.PutUint16(query[4:], 1)

	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}

	query = append(query, 0)
	query = binary.BigEndian.AppendUint16(query, qtype)
	query = binary.BigEndian.AppendUint16(query, dnsClassIN)

	return query
}

// parsePTRResponse возвращает имя из первой PTR-записи ответа.
func parsePTRResponse(msg []byte, id uint16) (string, error) {
	answers, offset, err := parseHeader(msg, id)
	if err != nil {
		return "", err
	}

	for range answers {
		if _, offset, err = readName(msg, offset); err != nil {
			return "", err
		}

		if offset+10 > len(msg) {
			return "", errors.New("short answer")
		}

		rtype := binary.BigEndian.Uint16(msg[offset:])
		length := int(binary.BigEndian.Uint16(msg[offset+8:]))
		offset += 10

		if offset+length > len(msg) {
			return "", errors.New("short answer data")
		}

		if rtype == dnsTypePTR {
			name, _, err := readName(msg, offset)

			return name, err
		}

		offset += length
	}

	return "", errors.New("no PTR record")
}

// parseNBStatResponse возвращает имя компьютера: уникальное имя с суффиксом 0x00 из таблицы NBSTAT.
func parseNBStatResponse(msg []byte, id uint16) (string, error) {
	answers, offset, err := parseHeader(msg, id)
	if err != nil {
		return "", err
	}

	if answers == 0 {
		return "", errors.New("no NBSTAT answer")
	}

	if _, offset, err = readName(msg, offset); err != nil {
		return "", err
	}

	// Тип, класс, TTL, длина данных и число имен.
	if offset+11 > len(msg) {
		return "", errors.New("short NBSTAT answer")
	}

	count := int(msg[offset+10])
	offset += 11

	for i := 0; i < count && offset+18 <= len(msg); i++ {
		entry := msg[offset : offset+18]
		offset += 18

		// Суффикс 0x00 — имя рабочей станции, флаг 0x8000 — групповое имя (рабочая группа).
		if entry[15] == 0x00 && binary.BigEndian.Uint16(entry[16:])&0x8000 == 0 {
			return strings.TrimRight(string(entry[:15]), " \x00"), nil
		}
	}

	return "", errors.New("no workstation name")
}

// parseHeader проверяет заголовок ответа и возвращает число ответов и смещение после раздела вопросов.
func parseHeader(msg []byte, id uint16) (int, int, error) {
	if len(msg) < 12 {
		return 0, 0, errors.New("short response")
	}

	// mDNS-ответ может прийти без ID запроса, остальные — только с ним.
	if got := binary.BigEndian.Uint16(msg[0:]); got != id && got != 0 {
		return 0, 0, errors.New("unexpected response id")
	}

	if msg[2]&0x80 == 0 {
		return 0, 0, errors.New("not a response")
	}

	if rcode := msg[3] & 0x0f; rcode != 0 {
		return 0, 0, fmt.Errorf("response code %d", rcode)
	}

	questions := int(binary.BigEndian.Uint16(msg[4:]))
	answers := int(binary.BigEndian.Uint16(msg[6:]))
	offset := 12

	for range questions {
		var err error

		if _, offset, err = readName(msg, offset); err != nil {
			return 0, 0, err
		}

		offset += 4
	}

	if offset > len(msg) {
		return 0, 0, errors.New("short question")
	}

	return answers, offset, nil
}

// readName читает имя DNS со сжатием (ссылками) и возвращает его и смещение после имени.
func readName(msg []byte, offset int) (string, int, error) {
	labels := make([]string, 0, 4)
	end := -1

	for jumps := 0; ; {
		if offset >= len(msg) {
			return "", 0, errors.New("name out of bounds")
		}

		length := int(msg[offset])

		switch {
		case length == 0:
			if end < 0 {
				end = offset + 1
			}

			return strings.Join(labels, "."), end, nil

		case length&0xc0 == 0xc0:
			if offset+1 >= len(msg) || jumps > 10 {
				return "", 0, errors.New("bad name pointer")
			}

			if end < 0 {
				end = offset + 2
			}

			offset = int(binary.BigEndian.Uint16(msg[offset:]) & 0x3fff)
			jumps++

		default:
			if offset+1+length > len(msg) {
				return "", 0, errors.New("label out of bounds")
			}

			labels = append(labels, string(msg[offset+1:offset+1+length]))
			offset += 1 + length
		}
	}
}

// reverseName возвращает имя для обратного запроса: 4.3.2.1.in-addr.arpa или полубайты IPv6 в ip6.arpa.
func reverseName(ip netip.Addr) string {
	ip = ip.Unmap()

	if ip.Is4() {
		octets := ip.As4()

		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", octets[3], octets[2], octets[1], octets[0])
	}

	bytes := ip.As16()
	nibbles := make([]string, 0, 32)

	for i := len(bytes) - 1; i >= 0; i-- {
		nibbles = append(nibbles, strconv.FormatUint(uint64(bytes[i]&0x0f), 16), strconv.FormatUint(uint64(bytes[i]>>4), 16))
	}

	return strings.Join(nibbles, ".") + ".ip6.arpa"
}

// normalizeHostname оставляет первую метку имени (без .lan, .local и домена роутера). Пустая строка — имени нет:
// пустое, слишком длинное или собранное из самого адреса (192-168-1-20).
func normalizeHostname(name string, ip netip.Addr) string {
	name = strings.TrimSpace(strings.TrimSuffix(name, "."))
	name, _, _ = strings.Cut(name, ".")
	name = strings.ToValidUTF8(name, "")

	if name == "" || strings.EqualFold(name, "localhost") || utf8.RuneCountInString(name) > maxHostnameLength {
		return ""
	}

	if ip.Is4() {
		octets := ip.As4()
		digits := fmt.Sprintf("%d-%d-%d-%d", octets[0], octets[1], octets[2], octets[3])

		if strings.Contains(strings.ReplaceAll(name, "_", "-"), digits) {
			return ""
		}
	}

	return name
}

// randomID возвращает случайный ID запроса.
func randomID() uint16 {
	var b [2]byte

	_, _ = rand.Read(b[:])

	return binary.BigEndian.Uint16(b[:])
}

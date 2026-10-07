package devices

//go:generate go run oui_generate.go

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ouiData — префиксы MAC реестра MA-L IEEE и короткие имена производителей: строки "BC3898\tПроизводитель".
//
//go:embed oui.txt.gz
var ouiData []byte

var (
	ouiOnce     sync.Once
	ouiPrefixes []uint32
	ouiVendors  []string
)

// Vendor возвращает производителя устройства по первым трем байтам MAC-адреса. Для случайного MAC
// и неизвестного префикса — пустая строка.
func Vendor(mac string) string {
	mac = NormalizeMAC(mac)
	if mac == "" || IsRandomMAC(mac) {
		return ""
	}

	prefix, err := strconv.ParseUint(strings.ReplaceAll(mac[:8], ":", ""), 16, 32)
	if err != nil {
		return ""
	}

	ouiOnce.Do(loadOUI)

	if index, found := slices.BinarySearch(ouiPrefixes, uint32(prefix)); found {
		return ouiVendors[index]
	}

	return ""
}

// IsRandomMAC проверяет, что MAC-адрес локально администрируемый: так телефоны и ноутбуки скрывают
// заводской адрес в сетях Wi-Fi.
func IsRandomMAC(mac string) bool {
	mac = NormalizeMAC(mac)
	if mac == "" {
		return false
	}

	first, err := strconv.ParseUint(mac[:2], 16, 8)

	return err == nil && first&0x02 != 0
}

// loadOUI распаковывает встроенный реестр в отсортированные срезы префиксов и имен.
func loadOUI() {
	reader, err := gzip.NewReader(bytes.NewReader(ouiData))
	if err != nil {
		log.Printf("Devices: cannot read OUI registry: %v", err)

		return
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		log.Printf("Devices: cannot read OUI registry: %v", err)

		return
	}

	lines := strings.Split(string(data), "\n")
	ouiPrefixes = make([]uint32, 0, len(lines))
	ouiVendors = make([]string, 0, len(lines))

	for _, line := range lines {
		hex, vendor, found := strings.Cut(line, "\t")
		if !found {
			continue
		}

		prefix, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			continue
		}

		ouiPrefixes = append(ouiPrefixes, uint32(prefix))
		ouiVendors = append(ouiVendors, vendor)
	}
}

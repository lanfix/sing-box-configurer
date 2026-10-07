//go:build ignore

// Генератор oui.txt.gz: скачивает реестр MA-L IEEE и сохраняет префиксы MAC с короткими именами производителей.
//
//	go generate ./internal/devices
package main

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ouiURL — реестр MA-L (24-битные префиксы) IEEE.
const ouiURL = "https://standards-oui.ieee.org/oui/oui.csv"

var (
	// parenthesesRe — пояснения в скобках: "LG Electronics (Mobile Communications)".
	parenthesesRe = regexp.MustCompile(`\s*\([^)]*\)`)

	// assignmentRe — префикс MA-L из шести шестнадцатеричных цифр.
	assignmentRe = regexp.MustCompile(`^[0-9A-F]{6}$`)

	// suffixes — слова в конце названия, которые не помогают узнать производителя.
	suffixes = map[string]bool{
		"inc": true, "incorporated": true, "co": true, "company": true, "ltd": true, "limited": true,
		"corp": true, "corporation": true, "corporate": true, "llc": true, "gmbh": true, "ag": true,
		"sa": true, "s.a": true, "bv": true, "b.v": true, "oy": true, "ab": true, "plc": true, "pty": true,
		"srl": true, "spa": true, "s.p.a": true, "kg": true, "as": true, "a/s": true, "nv": true, "sas": true,
		"technologies": true, "technology": true, "tech": true, "electronics": true, "electronic": true,
		"communications": true, "communication": true, "telecommunications": true, "international": true,
		"holdings": true, "group": true, "industrial": true, "industries": true, "ind": true, "&": true,
	}
)

func main() {
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	request, err := http.NewRequest(http.MethodGet, ouiURL, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Сервер IEEE отвечает 418 на User-Agent Go по умолчанию.
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; sing-box-configurer OUI generator)")

	response, err := client.Do(request)
	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", ouiURL, response.Status)
	}

	entries, err := parse(response.Body)
	if err != nil {
		log.Fatal(err)
	}

	if err = write("oui.txt.gz", entries); err != nil {
		log.Fatal(err)
	}

	log.Printf("oui.txt.gz: %d prefixes", len(entries))
}

// parse читает CSV реестра и возвращает строки "ПРЕФИКС\tпроизводитель", отсортированные по префиксу.
func parse(r io.Reader) ([]string, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	entries := make([]string, 0, len(records))

	for _, record := range records {
		if len(record) < 3 || !assignmentRe.MatchString(record[1]) {
			continue
		}

		// Блоки, поделенные на диапазоны MA-M и MA-S, производителя не называют.
		if name := shorten(record[2]); name != "" && name != "IEEE Registration Authority" {
			entries = append(entries, record[1]+"\t"+name)
		}
	}

	slices.Sort(entries)

	return slices.Compact(entries), nil
}

// shorten убирает из названия организации пояснения в скобках и юридические и общие слова в конце.
func shorten(name string) string {
	name = parenthesesRe.ReplaceAllString(strings.TrimSpace(name), "")

	words := strings.FieldsFunc(name, func(r rune) bool {
		return r == ' ' || r == ','
	})

	for len(words) > 1 && suffixes[strings.ToLower(strings.TrimRight(words[len(words)-1], "."))] {
		words = words[:len(words)-1]
	}

	return strings.TrimSpace(strings.Join(words, " "))
}

// write сохраняет строки в gzip-файл.
func write(path string, entries []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if _, err = fmt.Fprintln(writer, entry); err != nil {
			return err
		}
	}

	if err = writer.Close(); err != nil {
		return err
	}

	return file.Close()
}

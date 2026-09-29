package outbound

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Источники outbound-ов.
const (
	// SourceAll — outbound-ы всех источников.
	SourceAll     = "all"
	SourceManual  = "manual"
	SourceHapp    = "happ"
	SourceAmnezia = "amnezia"
)

// Параметры urltest-а по умолчанию.
const (
	DefaultTestURL   = "https://www.gstatic.com/generate_204"
	DefaultInterval  = "3m"
	DefaultTolerance = 100
)

// ErrURLTestNotFound — urltest-а с таким ID нет.
var ErrURLTestNotFound = errors.New("urltest not found")

// URLTestSource — источник участников urltest-а. Пустой ProfileID — все профили источника.
type URLTestSource struct {
	Kind      string `json:"kind"`
	ProfileID string `json:"profile_id,omitempty"`
}

// URLTest — urltest, участники которого подбираются по источникам и фильтрам при каждом рендере.
//
// Состав: outbound-ы источников Sources, чей тег подходит под IncludeRegexp, и явно указанные Tags.
// Из них исключаются теги ExcludeTags и теги, подходящие под ExcludeRegexp.
type URLTest struct {
	ID            string          `json:"id"`
	Tag           string          `json:"tag"`
	Description   string          `json:"description"`
	Sources       []URLTestSource `json:"sources"`
	IncludeRegexp string          `json:"include_regexp,omitempty"`
	ExcludeRegexp string          `json:"exclude_regexp,omitempty"`
	Tags          []string        `json:"tags"`
	ExcludeTags   []string        `json:"exclude_tags"`
	URL           string          `json:"url,omitempty"`
	Interval      string          `json:"interval,omitempty"`
	Tolerance     int             `json:"tolerance,omitempty"`

	InterruptExistConnections bool      `json:"interrupt_exist_connections"`
	CreatedAt                 time.Time `json:"created_at"`
}

// Subscription — outbound-ы одного профиля подписки (Happ, Amnezia).
type Subscription struct {
	Source      string
	ProfileID   string
	ProfileName string
	Outbounds   []map[string]any
}

// Candidate — outbound, который может войти в urltest.
type Candidate struct {
	Tag       string `json:"tag"`
	Type      string `json:"type"`
	Source    string `json:"source"`
	ProfileID string `json:"profile_id,omitempty"`
}

// MemberState — состояние outbound-а в составе urltest-а.
type MemberState string

const (
	MemberIncluded       MemberState = "included"
	MemberExcludedTag    MemberState = "excluded_tag"
	MemberExcludedRegexp MemberState = "excluded_regexp"

	// MemberMissing — явно указанного тега нет среди outbound-ов.
	MemberMissing MemberState = "missing"
)

// Member — outbound, подобранный urltest-ом, и его состояние.
type Member struct {
	Tag   string      `json:"tag"`
	State MemberState `json:"state"`
}

// DefaultURLTests возвращает urltest-ы новой инсталляции: auto из всех outbound-ов.
func DefaultURLTests() []URLTest {
	return []URLTest{
		{
			ID:          "auto",
			Tag:         AutoTag,
			Description: "Самый быстрый из всех outbound-ов",
			Sources: []URLTestSource{
				{
					Kind:      SourceAll,
					ProfileID: "",
				},
			},
			IncludeRegexp:             "",
			ExcludeRegexp:             "",
			Tags:                      []string{},
			ExcludeTags:               []string{},
			URL:                       DefaultTestURL,
			Interval:                  DefaultInterval,
			Tolerance:                 DefaultTolerance,
			InterruptExistConnections: false,
			CreatedAt:                 time.Now(),
		},
	}
}

// Normalize убирает пробелы, пустые и повторяющиеся значения.
func (u *URLTest) Normalize() {
	u.Tag = strings.TrimSpace(u.Tag)
	u.Description = strings.TrimSpace(u.Description)
	u.IncludeRegexp = strings.TrimSpace(u.IncludeRegexp)
	u.ExcludeRegexp = strings.TrimSpace(u.ExcludeRegexp)
	u.URL = strings.TrimSpace(u.URL)
	u.Interval = strings.TrimSpace(u.Interval)
	u.Tags = normalizeTags(u.Tags)
	u.ExcludeTags = normalizeTags(u.ExcludeTags)

	sources := make([]URLTestSource, 0, len(u.Sources))

	for _, source := range u.Sources {
		source.ProfileID = strings.TrimSpace(source.ProfileID)

		if source.Kind == SourceAll || source.Kind == SourceManual {
			source.ProfileID = ""
		}

		if !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}

	u.Sources = sources
}

// Validate проверяет параметры urltest-а (без проверки уникальности тега).
func (u *URLTest) Validate() error {
	if u.Tag == "" {
		return fmt.Errorf("у urltest-а должен быть тег")
	}

	if IsReservedTag(u.Tag) {
		return fmt.Errorf("тег %s зарезервирован (direct, block и select-*)", u.Tag)
	}

	for _, source := range u.Sources {
		if !slices.Contains([]string{SourceAll, SourceManual, SourceHapp, SourceAmnezia}, source.Kind) {
			return fmt.Errorf("неизвестный источник %q", source.Kind)
		}
	}

	if len(u.Sources) == 0 && len(u.Tags) == 0 {
		return fmt.Errorf("выберите источники или укажите теги outbound-ов")
	}

	if slices.Contains(u.Tags, u.Tag) {
		return fmt.Errorf("urltest не может включать сам себя")
	}

	if _, _, err := u.compile(); err != nil {
		return err
	}

	if u.URL != "" {
		if parsed, err := url.Parse(u.URL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("некорректный URL проверки %q", u.URL)
		}
	}

	if u.Interval != "" {
		if duration, err := time.ParseDuration(u.Interval); err != nil || duration <= 0 {
			return fmt.Errorf("некорректный интервал %q: ожидается длительность вида 3m или 30s", u.Interval)
		}
	}

	if u.Tolerance < 0 {
		return fmt.Errorf("tolerance не может быть отрицательным")
	}

	return nil
}

// Resolve подбирает участников из candidates. В результат попадают и исключенные outbound-ы,
// и явно указанные теги, которых нет среди candidates, — с соответствующим состоянием.
func (u *URLTest) Resolve(candidates []Candidate) ([]Member, error) {
	include, exclude, err := u.compile()
	if err != nil {
		return nil, err
	}

	members := make([]Member, 0)
	seen := map[string]bool{}

	state := func(tag string) MemberState {
		switch {
		case slices.Contains(u.ExcludeTags, tag):
			return MemberExcludedTag

		case exclude != nil && exclude.MatchString(tag):
			return MemberExcludedRegexp

		default:
			return MemberIncluded
		}
	}

	for _, candidate := range candidates {
		if seen[candidate.Tag] || !u.matchesSource(candidate) {
			continue
		}

		if include != nil && !include.MatchString(candidate.Tag) {
			continue
		}

		seen[candidate.Tag] = true
		members = append(members, Member{
			Tag:   candidate.Tag,
			State: state(candidate.Tag),
		})
	}

	for _, tag := range u.Tags {
		if seen[tag] {
			continue
		}

		seen[tag] = true

		if !slices.ContainsFunc(candidates, func(candidate Candidate) bool {
			return candidate.Tag == tag
		}) {
			members = append(members, Member{
				Tag:   tag,
				State: MemberMissing,
			})

			continue
		}

		members = append(members, Member{
			Tag:   tag,
			State: state(tag),
		})
	}

	return members, nil
}

// Config возвращает объект urltest-а sing-box с участниками members.
func (u *URLTest) Config(members []string) map[string]any {
	outbounds := make([]any, 0, len(members))

	for _, member := range members {
		outbounds = append(outbounds, member)
	}

	config := map[string]any{
		"type":                        "urltest",
		"tag":                         u.Tag,
		"outbounds":                   outbounds,
		"interrupt_exist_connections": u.InterruptExistConnections,
	}

	// Пустые параметры не выводятся: sing-box подставит свои значения по умолчанию.
	if u.URL != "" {
		config["url"] = u.URL
	}

	if u.Interval != "" {
		config["interval"] = u.Interval
	}

	if u.Tolerance > 0 {
		config["tolerance"] = u.Tolerance
	}

	return config
}

// matchesSource проверяет, что outbound входит в один из источников. Группы (selector, urltest)
// по источникам не подбираются — их можно добавить только явно.
func (u *URLTest) matchesSource(candidate Candidate) bool {
	if candidate.Type == "selector" || candidate.Type == "urltest" {
		return false
	}

	return slices.ContainsFunc(u.Sources, func(source URLTestSource) bool {
		switch {
		case source.Kind == SourceAll:
			return true

		case source.Kind != candidate.Source:
			return false

		default:
			return source.ProfileID == "" || source.ProfileID == candidate.ProfileID
		}
	})
}

// compile компилирует регулярные выражения фильтров. Пустое выражение — nil.
func (u *URLTest) compile() (*regexp.Regexp, *regexp.Regexp, error) {
	include, err := compileOptional(u.IncludeRegexp)
	if err != nil {
		return nil, nil, fmt.Errorf("некорректное выражение фильтра: %w", err)
	}

	exclude, err := compileOptional(u.ExcludeRegexp)
	if err != nil {
		return nil, nil, fmt.Errorf("некорректное выражение исключения: %w", err)
	}

	return include, exclude, nil
}

// IncludedTags возвращает теги участников, которые войдут в urltest.
func IncludedTags(members []Member) []string {
	tags := make([]string, 0, len(members))

	for _, member := range members {
		if member.State == MemberIncluded {
			tags = append(tags, member.Tag)
		}
	}

	return tags
}

// compileOptional компилирует выражение, если оно задано.
func compileOptional(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}

	return regexp.Compile(expr)
}

// normalizeTags убирает пробелы, пустые и повторяющиеся теги.
func normalizeTags(tags []string) []string {
	result := make([]string, 0, len(tags))

	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" && !slices.Contains(result, tag) {
			result = append(result, tag)
		}
	}

	return result
}

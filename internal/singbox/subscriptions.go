package singbox

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/lanfix/sing-box-configurer/internal/jsonmap"
	"github.com/lanfix/sing-box-configurer/internal/outbound"
	"github.com/lanfix/sing-box-configurer/internal/render"
	"github.com/lanfix/sing-box-configurer/internal/repository/singboxconfig"
)

// subscriptionKey — профиль подписки.
type subscriptionKey struct {
	source    string
	profileID string
}

// ApplySubscriptionUpdate переносит в рабочий конфиг обновленные серверы подписок и перезапускает sing-box.
// previous — состояние обновленных профилей до обновления. Остальные неприменённые изменения в рабочий
// конфиг не попадают и продолжают ждать применения. Профили, которых в рабочем конфиге еще нет
// (добавлены, но не применены), пропускаются. Если переносить нечего, возвращается ErrNoChanges.
func (s *Service) ApplySubscriptionUpdate(ctx context.Context, previous []outbound.Subscription) (*ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input, err := s.source()
	if err != nil {
		return nil, fmt.Errorf("cannot collect render input: %w", err)
	}

	actual, err := s.provider.GetActualConfigParsed()
	if err != nil {
		return nil, fmt.Errorf("cannot read sing-box config: %w", err)
	}

	// Те же данные, но с прежними серверами обновленных профилей: по ним видно, какие теги были у профилей.
	before := input
	before.Subscriptions = slices.Clone(input.Subscriptions)

	keys := map[subscriptionKey]bool{}

	for _, subscription := range previous {
		key := subscriptionKey{source: subscription.Source, profileID: subscription.ProfileID}
		keys[key] = true

		for i := range before.Subscriptions {
			if before.Subscriptions[i].Source == key.source && before.Subscriptions[i].ProfileID == key.profileID {
				before.Subscriptions[i] = subscription
			}
		}
	}

	oldTags := subscriptionTags(render.Candidates(before), keys)
	newTags := subscriptionTags(render.Candidates(input), keys)
	live := proxyTags(actual)

	// Профиль, ни одного сервера которого нет в рабочем конфиге, еще не применен: его изменения ждут применения.
	var removed, added []string

	for key := range keys {
		if !slices.ContainsFunc(oldTags[key], func(tag string) bool { return live[tag] }) {
			continue
		}

		removed = append(removed, oldTags[key]...)
		added = append(added, newTags[key]...)
	}

	if len(removed) == 0 && len(added) == 0 {
		return nil, ErrNoChanges
	}

	result, err := render.Render(input)
	if err != nil {
		return nil, err
	}

	// Отрендеренный конфиг приводится к виду конфига с диска: те же типы значений после JSON.
	raw, err := singboxconfig.Marshal(result.Config)
	if err != nil {
		return nil, err
	}

	rendered, err := singboxconfig.Parse(raw)
	if err != nil {
		return nil, err
	}

	if err = patchSubscriptions(actual, rendered, removed, added); err != nil {
		return nil, err
	}

	data, err := singboxconfig.Marshal(actual)
	if err != nil {
		return nil, err
	}

	if current, _ := s.normalizedActual(); bytes.Equal(data, current) {
		return nil, ErrNoChanges
	}

	applied, err := s.applyLocked(ctx, data, nil)
	if err != nil {
		return nil, err
	}

	applied.Message = "Обновленные серверы перенесены в работающий sing-box (остальные изменения ждут применения)"

	return applied, nil
}

// subscriptionTags возвращает теги outbound-ов профилей keys в том виде, в котором они попадут в конфиг.
func subscriptionTags(candidates []outbound.Candidate, keys map[subscriptionKey]bool) map[subscriptionKey][]string {
	result := map[subscriptionKey][]string{}

	for _, candidate := range candidates {
		key := subscriptionKey{source: candidate.Source, profileID: candidate.ProfileID}

		if keys[key] {
			result[key] = append(result[key], candidate.Tag)
		}
	}

	return result
}

// proxyTags возвращает теги outbound-ов и endpoint-ов конфига.
func proxyTags(config map[string]any) map[string]bool {
	tags := map[string]bool{}

	for _, key := range []string{"outbounds", "endpoints"} {
		items, _ := config[key].([]any)

		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				tags[jsonmap.String(object, "tag")] = true
			}
		}
	}

	return tags
}

// patchSubscriptions заменяет в рабочем конфиге actual outbound-ы и endpoint-ы с тегами oldTags на outbound-ы
// с тегами newTags из отрендеренного конфига rendered и обновляет участников selector-ов и urltest-ов.
// Остальное в actual не меняется: неприменённые изменения из rendered туда не попадают.
func patchSubscriptions(actual, rendered map[string]any, oldTags, newTags []string) error {
	replaced := map[string]bool{}
	removed := map[string]bool{}
	added := map[string]bool{}

	for _, tag := range oldTags {
		replaced[tag] = true

		if !slices.Contains(newTags, tag) {
			removed[tag] = true
		}
	}

	for _, tag := range newTags {
		replaced[tag] = true

		if !slices.Contains(oldTags, tag) {
			added[tag] = true
		}
	}

	for _, key := range []string{"outbounds", "endpoints"} {
		replaceItems(actual, rendered, key, replaced, newTags)
	}

	renderedGroups := map[string]map[string]any{}
	renderedItems, _ := rendered["outbounds"].([]any)

	for _, item := range renderedItems {
		if object, ok := item.(map[string]any); ok && isGroup(object) {
			renderedGroups[jsonmap.String(object, "tag")] = object
		}
	}

	items, _ := actual["outbounds"].([]any)

	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || !isGroup(object) {
			continue
		}

		tag := jsonmap.String(object, "tag")
		itemType := jsonmap.String(object, "type")
		current := jsonmap.Strings(object, "outbounds")

		var renderedMembers []string

		renderedGroup := renderedGroups[tag]

		if renderedGroup != nil && jsonmap.String(renderedGroup, "type") == itemType {
			renderedMembers = jsonmap.Strings(renderedGroup, "outbounds")
		}

		// Группы, которых обновление не касается, не трогаются: иначе в них попал бы неприменённый порядок участников.
		touched := slices.ContainsFunc(current, func(member string) bool { return removed[member] }) ||
			slices.ContainsFunc(renderedMembers, func(member string) bool { return added[member] })

		if !touched {
			continue
		}

		members := mergeMembers(current, renderedMembers, removed, added)

		if len(members) == 0 {
			return fmt.Errorf("в %s %s не осталось outbound-ов: примените конфиг целиком", itemType, tag)
		}

		object["outbounds"] = toAnySlice(members)

		if itemType != "selector" {
			continue
		}

		if value := jsonmap.String(object, "default"); value != "" && !slices.Contains(members, value) {
			object["default"] = members[0]

			if fallback := jsonmap.String(renderedGroup, "default"); slices.Contains(members, fallback) {
				object["default"] = fallback
			}
		}
	}

	return nil
}

// replaceItems заменяет в списке key рабочего конфига элементы с тегами replaced на элементы с тегами newTags
// из отрендеренного конфига. Новые элементы встают на место первого замененного, а если таких нет — перед
// первым selector-ом, urltest-ом или встроенным outbound-ом.
func replaceItems(actual, rendered map[string]any, key string, replaced map[string]bool, newTags []string) {
	items, _ := actual[key].([]any)
	renderedItems, _ := rendered[key].([]any)
	fresh := make([]any, 0, len(newTags))

	for _, item := range renderedItems {
		if object, ok := item.(map[string]any); ok && slices.Contains(newTags, jsonmap.String(object, "tag")) {
			fresh = append(fresh, object)
		}
	}

	result := make([]any, 0, len(items)+len(fresh))
	inserted := false

	for _, item := range items {
		object, _ := item.(map[string]any)

		if replaced[jsonmap.String(object, "tag")] {
			if !inserted {
				result = append(result, fresh...)
				inserted = true
			}

			continue
		}

		if itemType := jsonmap.String(object, "type"); !inserted && (isGroup(object) || itemType == "direct" || itemType == "block") {
			result = append(result, fresh...)
			inserted = true
		}

		result = append(result, item)
	}

	if !inserted {
		result = append(result, fresh...)
	}

	if len(result) == 0 {
		delete(actual, key)

		return
	}

	actual[key] = result
}

// mergeMembers возвращает участников группы рабочего конфига current после обновления подписок: без удаленных
// серверов и с новыми — на их месте из отрендеренной группы rendered. Остальные участники остаются как были.
func mergeMembers(current, rendered []string, removed, added map[string]bool) []string {
	result := make([]string, 0, len(current)+len(added))

	for _, tag := range rendered {
		keep := added[tag] || (slices.Contains(current, tag) && !removed[tag])

		if keep && !slices.Contains(result, tag) {
			result = append(result, tag)
		}
	}

	for _, tag := range current {
		if !removed[tag] && !slices.Contains(result, tag) {
			result = append(result, tag)
		}
	}

	return result
}

// isGroup проверяет, что outbound — selector или urltest.
func isGroup(object map[string]any) bool {
	itemType := jsonmap.String(object, "type")

	return itemType == "selector" || itemType == "urltest"
}

// toAnySlice преобразует список строк в JSON-массив.
func toAnySlice(values []string) []any {
	result := make([]any, 0, len(values))

	for _, value := range values {
		result = append(result, value)
	}

	return result
}

// Package jsonmap содержит операции над JSON-объектами, разобранными в map[string]any.
package jsonmap

import (
	"encoding/json"
)

// Clone возвращает глубокую копию значения, разобранного из JSON (map, slice и скалярные значения).
func Clone[T any](value T) T {
	return any(cloneValue(value)).(T)
}

// cloneValue рекурсивно копирует map[string]any и []any.
func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))

		for key, item := range typed {
			result[key] = cloneValue(item)
		}

		return result

	case []any:
		result := make([]any, len(typed))

		for i, item := range typed {
			result[i] = cloneValue(item)
		}

		return result

	case []map[string]any:
		result := make([]map[string]any, len(typed))

		for i, item := range typed {
			result[i] = cloneValue(item).(map[string]any)
		}

		return result

	default:
		return value
	}
}

// Merge дописывает в dst поля из src. Вложенные объекты объединяются рекурсивно, остальные значения
// из src заменяют значения dst. dst изменяется на месте.
func Merge(dst, src map[string]any) {
	for key, value := range src {
		srcMap, srcIsMap := value.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)

		if srcIsMap && dstIsMap {
			Merge(dstMap, srcMap)

			continue
		}

		dst[key] = cloneValue(value)
	}
}

// Normalize приводит значение к виду, который дает json.Unmarshal в any: числа становятся float64,
// структуры — map[string]any. Нужен, чтобы сравнивать и копировать значения единообразно.
func Normalize(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var result any

	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// Int возвращает целое значение поля key (JSON-числа разбираются как float64).
func Int(data map[string]any, key string) int {
	switch value := data[key].(type) {
	case float64:
		return int(value)

	case int:
		return value

	default:
		return 0
	}
}

// String возвращает строковое значение поля key.
func String(data map[string]any, key string) string {
	value, _ := data[key].(string)

	return value
}

// Bool возвращает логическое значение поля key.
func Bool(data map[string]any, key string) bool {
	value, _ := data[key].(bool)

	return value
}

// Strings возвращает значение поля key как список строк (одиночная строка превращается в список).
func Strings(data map[string]any, key string) []string {
	switch value := data[key].(type) {
	case string:
		return []string{value}

	case []any:
		result := make([]string, 0, len(value))

		for _, item := range value {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}

		return result

	case []string:
		return append([]string{}, value...)

	default:
		return nil
	}
}

// Without возвращает копию data без полей keys.
func Without(data map[string]any, keys ...string) map[string]any {
	result := Clone(data)

	for _, key := range keys {
		delete(result, key)
	}

	return result
}

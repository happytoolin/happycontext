package hc

import "strings"

const defaultRedactedValue = "[REDACTED]"

// FieldMapper transforms or drops finalized fields before they are written.
//
// Return keep=false to omit the field.
type FieldMapper func(key string, value any) (mapped any, keep bool)

// ChainFieldMappers composes field mappers in declaration order.
func ChainFieldMappers(mappers ...FieldMapper) FieldMapper {
	filtered := make([]FieldMapper, 0, len(mappers))
	for _, mapper := range mappers {
		if mapper != nil {
			filtered = append(filtered, mapper)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	return func(key string, value any) (any, bool) {
		current := value
		for _, mapper := range filtered {
			mapped, keep := mapper(key, current)
			if !keep {
				return nil, false
			}
			current = mapped
		}
		return current, true
	}
}

// RedactKeys replaces exact key matches with the default redacted value.
func RedactKeys(keys ...string) FieldMapper {
	return RedactKeysValue(defaultRedactedValue, keys...)
}

// RedactKeysValue replaces exact key matches with redactedValue.
func RedactKeysValue(redactedValue any, keys ...string) FieldMapper {
	keySet := fieldKeySet(keys)
	if len(keySet) == 0 {
		return nil
	}
	return func(key string, value any) (any, bool) {
		if _, ok := keySet[key]; ok {
			return redactedValue, true
		}
		return value, true
	}
}

// RedactKeyPrefixes replaces fields whose key starts with any prefix.
func RedactKeyPrefixes(prefixes ...string) FieldMapper {
	filtered := nonEmptyStrings(prefixes)
	if len(filtered) == 0 {
		return nil
	}
	return func(key string, value any) (any, bool) {
		for _, prefix := range filtered {
			if strings.HasPrefix(key, prefix) {
				return defaultRedactedValue, true
			}
		}
		return value, true
	}
}

// DropKeys removes exact key matches.
func DropKeys(keys ...string) FieldMapper {
	keySet := fieldKeySet(keys)
	if len(keySet) == 0 {
		return nil
	}
	return func(key string, value any) (any, bool) {
		if _, ok := keySet[key]; ok {
			return nil, false
		}
		return value, true
	}
}

func applyFieldMapper(fields map[string]any, mapper FieldMapper) map[string]any {
	if mapper == nil || len(fields) == 0 {
		return fields
	}
	for key, value := range fields {
		mapped, keep := mapper(key, value)
		if !keep {
			delete(fields, key)
			continue
		}
		fields[key] = mapped
	}
	return fields
}

func fieldKeySet(keys []string) map[string]struct{} {
	filtered := nonEmptyStrings(keys)
	if len(filtered) == 0 {
		return nil
	}
	keySet := make(map[string]struct{}, len(filtered))
	for _, key := range filtered {
		keySet[key] = struct{}{}
	}
	return keySet
}

func nonEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

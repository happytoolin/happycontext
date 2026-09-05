package hc

import (
	"errors"
	"fmt"
	"reflect"
)

func structuredErrorField(err error) map[string]any {
	if err == nil {
		return nil
	}
	// Typed-nil errors (non-nil interface, nil pointer) must not reach
	// Error()/Unwrap(): they panic on nil dereference and would crash
	// finalization. fmt renders them safely as "<nil>".
	if isTypedNilError(err) {
		return map[string]any{
			"message": fmt.Sprint(err),
			"type":    fmt.Sprintf("%T", err),
		}
	}
	field := map[string]any{
		"message": structuredErrorMessage(err),
		"type":    fmt.Sprintf("%T", err),
	}

	if cause := deepestUnwrappedError(err); cause != nil && !sameError(cause, err) {
		field["cause.message"] = structuredErrorMessage(cause)
		field["cause.type"] = fmt.Sprintf("%T", cause)
	}

	return field
}

// isTypedNilError reports whether err is a non-nil interface holding
// a nil pointer — the value whose Error() call would panic.
func isTypedNilError(err error) bool {
	if err == nil {
		return false
	}
	v := reflect.ValueOf(err)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func structuredPanicField(recovered any) map[string]any {
	return map[string]any{
		"type":  fmt.Sprintf("%T", recovered),
		"value": fmt.Sprint(recovered),
	}
}

func structuredErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	if message, ok := frameworkStyleErrorMessage(err); ok {
		return message
	}

	return err.Error()
}

func deepestUnwrappedError(err error) error {
	current := err
	seen := make(map[error]struct{})
	for depth := 0; current != nil; depth++ {
		if depth >= 100 {
			return current
		}
		if isComparableError(current) {
			if _, ok := seen[current]; ok {
				return current
			}
			seen[current] = struct{}{}
		}
		next := errors.Unwrap(current)
		if next == nil {
			return current
		}
		current = next
	}
	return nil
}

func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	if !isComparableError(a) {
		return false
	}
	return a == b
}

func isComparableError(err error) bool {
	if err == nil {
		return true
	}
	return reflect.TypeOf(err).Comparable()
}

func frameworkStyleErrorMessage(err error) (string, bool) {
	value := reflect.ValueOf(err)
	if !value.IsValid() {
		return "", false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", false
	}

	codeField := value.FieldByName("Code")
	messageField := value.FieldByName("Message")
	if !codeField.IsValid() || !messageField.IsValid() {
		return "", false
	}
	if !isIntKind(codeField.Kind()) {
		return "", false
	}

	message, ok := messageValue(messageField)
	if !ok {
		return "", false
	}
	text := fmt.Sprint(message)
	if text == "" {
		return "", false
	}
	return text, true
}

func messageValue(field reflect.Value) (any, bool) {
	if !field.IsValid() {
		return nil, false
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil, false
		}
		field = field.Elem()
	}
	if !field.CanInterface() {
		return nil, false
	}

	value := field.Interface()
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, false
		}
		return v, true
	case fmt.Stringer:
		text := v.String()
		if text == "" {
			return nil, false
		}
		return text, true
	default:
		if field.Kind() == reflect.Interface && !field.IsNil() {
			inner := field.Elem()
			if inner.IsValid() && inner.CanInterface() {
				return inner.Interface(), true
			}
		}
		return value, true
	}
}

func isIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

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
	field := map[string]any{
		"message": structuredErrorMessage(err),
		"type":    fmt.Sprintf("%T", err),
	}

	if cause := deepestUnwrappedError(err); cause != nil && cause != err {
		field["cause.message"] = structuredErrorMessage(cause)
		field["cause.type"] = fmt.Sprintf("%T", cause)
	}

	return field
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
	for {
		next := errors.Unwrap(current)
		if next == nil {
			return current
		}
		current = next
	}
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
		if v.String() == "" {
			return nil, false
		}
		return v.String(), true
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

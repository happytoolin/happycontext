package hc

import (
	"reflect"
)

// deepCopyValue clones the mutable containers TestSink can retain
// (map[string]any, []any, and — via reflect — other map/slice kinds) so
// captures survive caller mutation. Immutable scalars return unchanged.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return deepCopyMap(val, newVisitSet())
	case []any:
		return deepCopySlice(val, newVisitSet())
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			if rv.CanInterface() {
				cp := reflect.New(rv.Type()).Elem()
				seen := map[uintptr]reflect.Value{}
				deepCopyReflect(rv, cp, seen)
				return cp.Interface()
			}
		}
		return v
	}
}

func deepCopyReflect(src, dst reflect.Value, seen map[uintptr]reflect.Value) {
	switch k := src.Kind(); k {
	case reflect.Map:
		if prior, ok := seen[src.Pointer()]; ok {
			dst.Set(prior)
			return
		}
		dst.Set(reflect.MakeMapWithSize(src.Type(), src.Len()))
		seen[src.Pointer()] = dst
		for _, k := range src.MapKeys() {
			ev := reflect.New(src.Type().Elem()).Elem()
			deepCopyReflect(src.MapIndex(k), ev, seen)
			dst.SetMapIndex(k, ev)
		}
	case reflect.Slice:
		if prior, ok := seen[src.Pointer()]; ok {
			dst.Set(prior)
			return
		}
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))
		seen[src.Pointer()] = dst
		for i := 0; i < src.Len(); i++ {
			deepCopyReflect(src.Index(i), dst.Index(i), seen)
		}
	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			deepCopyReflect(src.Index(i), dst.Index(i), seen)
		}
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		if prior, ok := seen[src.Pointer()]; ok {
			dst.Set(prior)
			return
		}
		cp := reflect.New(src.Type().Elem())
		seen[src.Pointer()] = cp
		deepCopyReflect(src.Elem(), cp.Elem(), seen)
		dst.Set(cp)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		ev := reflect.New(src.Elem().Type()).Elem()
		deepCopyReflect(src.Elem(), ev, seen)
		dst.Set(ev)
	default:
		dst.Set(src)
	}
}

type visitSet struct {
	seen map[visitKey]any
}

type visitKey struct {
	typ reflect.Type
	ptr uintptr
}

func newVisitSet() *visitSet { return &visitSet{seen: map[visitKey]any{}} }

func deepCopyMap(m map[string]any, seen *visitSet) map[string]any {
	key := visitKey{typ: reflect.TypeOf(m), ptr: reflect.ValueOf(m).Pointer()}
	if prior, ok := seen.seen[key]; ok {
		return prior.(map[string]any)
	}
	out := make(map[string]any, len(m))
	seen.seen[key] = out
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopySlice(s []any, seen *visitSet) []any {
	key := visitKey{typ: reflect.TypeOf(s), ptr: reflect.ValueOf(s).Pointer()}
	if prior, ok := seen.seen[key]; ok {
		return prior.([]any)
	}
	out := make([]any, len(s))
	seen.seen[key] = out
	for i, v := range s {
		out[i] = deepCopyValue(v)
	}
	return out
}

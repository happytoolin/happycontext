package hc

import (
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestEventConcurrentAdd(t *testing.T) {
	e := newEvent()
	const n = 100

	wg := sync.WaitGroup{}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			e.addKV("k"+strconv.Itoa(i), i)
		}(i)
	}
	wg.Wait()

	s := e.snapshot()
	if len(s.fields) != n {
		t.Fatalf("expected %d fields, got %d", n, len(s.fields))
	}
}

func TestSetError(t *testing.T) {
	e := newEvent()
	e.setError(errors.New("x"))

	s := e.snapshot()
	if !s.hasError {
		t.Fatalf("expected HasError=true")
	}
	errField, ok := s.fields["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error field")
	}
	if errField["message"] != "x" {
		t.Fatalf("unexpected error message: %v", errField["message"])
	}
}

func TestSetErrorNilDoesNothing(t *testing.T) {
	e := newEvent()
	e.setError(nil)
	if e.hasErrorValue() {
		t.Fatalf("expected HasError=false")
	}
}

func TestSetRouteEmptyIgnored(t *testing.T) {
	e := newEvent()
	e.setRoute("")
	if _, ok := e.snapshot().fields["http.route"]; ok {
		t.Fatalf("did not expect route field")
	}
}

func TestEventStartTimeAndHasError(t *testing.T) {
	e := newEvent()
	if e.hasErrorValue() {
		t.Fatalf("expected no error initially")
	}
	if e.startedAt().IsZero() {
		t.Fatalf("expected non-zero start time")
	}
	e.setError(errors.New("boom"))
	if !e.hasErrorValue() {
		t.Fatalf("expected has error")
	}
}

func TestSetLevelInvalidIgnored(t *testing.T) {
	e := newEvent()
	if e.setLevel(Level("TRACE")) {
		t.Fatalf("expected invalid level to be rejected")
	}
	if _, ok := e.requestedLevelValue(); ok {
		t.Fatalf("expected invalid level to be ignored")
	}

	if !e.setLevel(LevelDebug) {
		t.Fatalf("expected valid level to be accepted")
	}
	level, ok := e.requestedLevelValue()
	if !ok || level != LevelDebug {
		t.Fatalf("expected debug override, got %q (ok=%v)", level, ok)
	}
}

func TestSnapshotCopiesTopLevelMap(t *testing.T) {
	e := newEvent()
	nested := map[string]any{
		"id": "u_1",
		"roles": []any{
			"admin",
			map[string]any{"scope": "billing"},
		},
	}
	e.addKV("user", nested)

	s := e.snapshot()

	user, ok := s.fields["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map in snapshot, got %T", s.fields["user"])
	}
	if user["id"] != "u_1" {
		t.Fatalf("expected user.id=u_1, got %v", user["id"])
	}
	snapshotRoles := user["roles"].([]any)
	if snapshotRoles[0] != "admin" {
		t.Fatalf("expected role[0]=admin, got %v", snapshotRoles[0])
	}
	if snapshotRoles[1].(map[string]any)["scope"] != "billing" {
		t.Fatalf("expected nested map value, got %v", snapshotRoles[1].(map[string]any)["scope"])
	}
}

func TestSnapshotSupportsCyclicMapValue(t *testing.T) {
	e := newEvent()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	cyclic["name"] = "root"

	e.addKV("node", cyclic)
	s := e.snapshot()

	node, ok := s.fields["node"].(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", s.fields["node"])
	}
	if node["name"] != "root" {
		t.Fatalf("expected name=root, got %v", node["name"])
	}
	self, ok := node["self"].(map[string]any)
	if !ok {
		t.Fatalf("expected self map, got %T", node["self"])
	}
	if self["name"] != "root" {
		t.Fatalf("expected self reference to preserve fields, got %v", self["name"])
	}
	if reflect.ValueOf(self).Pointer() != reflect.ValueOf(cyclic).Pointer() {
		t.Fatalf("expected shallow snapshot to preserve original cycle map")
	}
	if reflect.ValueOf(node).Pointer() != reflect.ValueOf(cyclic).Pointer() {
		t.Fatalf("expected shallow snapshot to share top-level map values")
	}
}

func TestSnapshotSharesNestedValues(t *testing.T) {
	e := newEvent()
	nested := map[string]any{
		"id": "u_1",
		"roles": []any{
			"admin",
			map[string]any{"scope": "billing"},
		},
	}
	e.addKV("user", nested)
	s := e.snapshot()

	nested["id"] = "u_2"
	roles := nested["roles"].([]any)
	roles[0] = "viewer"
	roles[1].(map[string]any)["scope"] = "support"

	user, ok := s.fields["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map in snapshot, got %T", s.fields["user"])
	}
	if user["id"] != "u_2" {
		t.Fatalf("expected shared user.id=u_2, got %v", user["id"])
	}
	snapshotRoles := user["roles"].([]any)
	if snapshotRoles[0] != "viewer" {
		t.Fatalf("expected shared role[0]=viewer, got %v", snapshotRoles[0])
	}
	if snapshotRoles[1].(map[string]any)["scope"] != "support" {
		t.Fatalf("expected shared nested map value, got %v", snapshotRoles[1].(map[string]any)["scope"])
	}
}

type structPayload struct {
	Meta map[string]int
	Tags []string
}

func TestSnapshotSharesStructReferenceFields(t *testing.T) {
	e := newEvent()
	p := structPayload{
		Meta: map[string]int{"count": 1},
		Tags: []string{"a", "b"},
	}
	e.addKV("payload", p)

	s := e.snapshot()
	p.Meta["count"] = 99
	p.Tags[0] = "z"

	got, ok := s.fields["payload"].(structPayload)
	if !ok {
		t.Fatalf("expected struct payload, got %T", s.fields["payload"])
	}
	if got.Meta["count"] != 99 {
		t.Fatalf("expected shared map value=99, got %d", got.Meta["count"])
	}
	if got.Tags[0] != "z" {
		t.Fatalf("expected shared slice value=z, got %s", got.Tags[0])
	}
}

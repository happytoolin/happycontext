package hc

import (
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestEventConcurrentAdd(t *testing.T) {
	e := NewEvent()
	const n = 100

	wg := sync.WaitGroup{}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			e.Add("k"+strconv.Itoa(i), i)
		}(i)
	}
	wg.Wait()

	s := e.Snapshot()
	if len(s.Fields) != n {
		t.Fatalf("expected %d fields, got %d", n, len(s.Fields))
	}
}

func TestSetError(t *testing.T) {
	e := NewEvent()
	e.SetError(errors.New("x"))

	s := e.Snapshot()
	if !s.HasError {
		t.Fatalf("expected HasError=true")
	}
	errField, ok := s.Fields["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error field")
	}
	if errField["message"] != "x" {
		t.Fatalf("unexpected error message: %v", errField["message"])
	}
}

func TestSetErrorNilDoesNothing(t *testing.T) {
	e := NewEvent()
	e.SetError(nil)
	if e.HasError() {
		t.Fatalf("expected HasError=false")
	}
}

func TestSetRouteEmptyIgnored(t *testing.T) {
	e := NewEvent()
	e.setRoute("")
	if _, ok := e.Snapshot().Fields["http.route"]; ok {
		t.Fatalf("did not expect route field")
	}
}

func TestEventStartTimeAndHasError(t *testing.T) {
	e := NewEvent()
	if e.HasError() {
		t.Fatalf("expected no error initially")
	}
	if e.StartTime().IsZero() {
		t.Fatalf("expected non-zero start time")
	}
	e.SetError(errors.New("boom"))
	if !e.HasError() {
		t.Fatalf("expected has error")
	}
}

func TestSetLevelInvalidIgnored(t *testing.T) {
	e := NewEvent()
	e.SetLevel("TRACE")
	if _, ok := e.RequestedLevel(); ok {
		t.Fatalf("expected invalid level to be ignored")
	}

	e.SetLevel(LevelDebug)
	level, ok := e.RequestedLevel()
	if !ok || level != LevelDebug {
		t.Fatalf("expected debug override, got %q (ok=%v)", level, ok)
	}
}

func TestSnapshotCopiesTopLevelMap(t *testing.T) {
	e := NewEvent()
	nested := map[string]any{
		"id": "u_1",
		"roles": []any{
			"admin",
			map[string]any{"scope": "billing"},
		},
	}
	e.Add("user", nested)

	s := e.Snapshot()

	user, ok := s.Fields["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map in snapshot, got %T", s.Fields["user"])
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
	e := NewEvent()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	cyclic["name"] = "root"

	e.Add("node", cyclic)
	s := e.Snapshot()

	node, ok := s.Fields["node"].(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", s.Fields["node"])
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
	if reflect.ValueOf(self).Pointer() != reflect.ValueOf(node).Pointer() {
		t.Fatalf("expected cycle to point to copied node map")
	}
	if reflect.ValueOf(node).Pointer() == reflect.ValueOf(cyclic).Pointer() {
		t.Fatalf("expected snapshot map to be a deep copy")
	}
}

func TestSnapshotDeepCopiesNestedValues(t *testing.T) {
	e := NewEvent()
	nested := map[string]any{
		"id": "u_1",
		"roles": []any{
			"admin",
			map[string]any{"scope": "billing"},
		},
	}
	e.Add("user", nested)
	s := e.Snapshot()

	nested["id"] = "u_2"
	roles := nested["roles"].([]any)
	roles[0] = "viewer"
	roles[1].(map[string]any)["scope"] = "support"

	user, ok := s.Fields["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user map in snapshot, got %T", s.Fields["user"])
	}
	if user["id"] != "u_1" {
		t.Fatalf("expected independent user.id=u_1, got %v", user["id"])
	}
	snapshotRoles := user["roles"].([]any)
	if snapshotRoles[0] != "admin" {
		t.Fatalf("expected independent role[0]=admin, got %v", snapshotRoles[0])
	}
	if snapshotRoles[1].(map[string]any)["scope"] != "billing" {
		t.Fatalf("expected independent nested map value, got %v", snapshotRoles[1].(map[string]any)["scope"])
	}
}

type structPayload struct {
	Meta map[string]int
	Tags []string
}

func TestSnapshotDeepCopiesStructReferenceFields(t *testing.T) {
	e := NewEvent()
	p := structPayload{
		Meta: map[string]int{"count": 1},
		Tags: []string{"a", "b"},
	}
	e.Add("payload", p)

	s := e.Snapshot()
	p.Meta["count"] = 99
	p.Tags[0] = "z"

	got, ok := s.Fields["payload"].(structPayload)
	if !ok {
		t.Fatalf("expected struct payload, got %T", s.Fields["payload"])
	}
	if got.Meta["count"] != 1 {
		t.Fatalf("expected copied map value=1, got %d", got.Meta["count"])
	}
	if got.Tags[0] != "a" {
		t.Fatalf("expected copied slice value=a, got %s", got.Tags[0])
	}
}

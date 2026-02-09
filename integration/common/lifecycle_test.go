package common

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/happytoolin/hlog"
)

func TestStartRequestAddsBaseFields(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "GET", "/orders/1")
	if event == nil || ctx == nil {
		t.Fatal("expected context and event")
	}
	snapshot := event.Snapshot()
	if snapshot.Fields["http.method"] != "GET" {
		t.Fatalf("method field = %v", snapshot.Fields["http.method"])
	}
	if snapshot.Fields["http.path"] != "/orders/1" {
		t.Fatalf("path field = %v", snapshot.Fields["http.path"])
	}
}

func TestFinalizeRequestEarlyReturnGuards(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "GET", "/x")
	sink := hlog.NewTestSink()

	FinalizeRequest(hlog.Config{}, FinalizeInput{Ctx: ctx, Event: event, StatusCode: 200})
	FinalizeRequest(hlog.Config{Sink: sink}, FinalizeInput{Ctx: nil, Event: event, StatusCode: 200})
	FinalizeRequest(hlog.Config{Sink: sink}, FinalizeInput{Ctx: ctx, Event: nil, StatusCode: 200})

	if len(sink.Events()) != 0 {
		t.Fatal("expected no writes from guarded paths")
	}
}

func TestFinalizeRequestRespectsSamplingDrop(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "GET", "/x")
	sink := hlog.NewTestSink()
	cfg := NormalizeConfig(hlog.Config{Sink: sink, SamplingRate: 0})

	FinalizeRequest(cfg, FinalizeInput{
		Ctx:        ctx,
		Event:      event,
		Method:     "GET",
		Path:       "/x",
		StatusCode: 200,
	})

	if len(sink.Events()) != 0 {
		t.Fatal("expected no event due to sampling")
	}
}

func TestFinalizeRequestMarksErrorAndRoute(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "POST", "/payments")
	sink := hlog.NewTestSink()
	cfg := NormalizeConfig(hlog.Config{Sink: sink, SamplingRate: 1})

	FinalizeRequest(cfg, FinalizeInput{
		Ctx:        ctx,
		Event:      event,
		Method:     "POST",
		Path:       "/payments",
		Route:      "/payments/:id",
		StatusCode: 200,
		Err:        errors.New("handler failed"),
	})

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hlog.LevelError {
		t.Fatalf("level = %s, want %s", events[0].Level, hlog.LevelError)
	}
	if events[0].Fields["http.route"] != "/payments/:id" {
		t.Fatalf("route = %v", events[0].Fields["http.route"])
	}
	if _, ok := events[0].Fields["error"].(map[string]any); !ok {
		t.Fatal("expected structured error field")
	}
}

func TestFinalizeRequestPanicAddsMetadata(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "GET", "/panic")
	sink := hlog.NewTestSink()
	cfg := NormalizeConfig(hlog.Config{Sink: sink, SamplingRate: 1})

	FinalizeRequest(cfg, FinalizeInput{
		Ctx:        ctx,
		Event:      event,
		Method:     "GET",
		Path:       "/panic",
		StatusCode: 500,
		Recovered:  "boom",
	})

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].Fields["panic"].(map[string]any); !ok {
		t.Fatal("expected panic field")
	}
	if events[0].Level != hlog.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestFinalizeRequestAppliesRequestedLevelFloor(t *testing.T) {
	ctx, event := StartRequest(context.Background(), "GET", "/x")
	hlog.SetLevel(ctx, hlog.LevelWarn)
	sink := hlog.NewTestSink()
	cfg := NormalizeConfig(hlog.Config{Sink: sink, SamplingRate: 1})

	FinalizeRequest(cfg, FinalizeInput{
		Ctx:        ctx,
		Event:      event,
		Method:     "GET",
		Path:       "/x",
		StatusCode: 200,
	})

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hlog.LevelWarn {
		t.Fatalf("level = %s, want WARN", events[0].Level)
	}
}

func TestResolveStatus(t *testing.T) {
	if got := ResolveStatus(0, nil, nil, false, 0); got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
	if got := ResolveStatus(http.StatusOK, errors.New("boom"), nil, false, 0); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", got, http.StatusInternalServerError)
	}
	if got := ResolveStatus(http.StatusOK, errors.New("boom"), nil, false, http.StatusBadRequest); got != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
	}
	if got := ResolveStatus(http.StatusCreated, errors.New("boom"), nil, true, http.StatusBadRequest); got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := ResolveStatus(http.StatusOK, nil, "panic", false, 0); got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", got, http.StatusInternalServerError)
	}
}

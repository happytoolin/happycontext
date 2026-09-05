package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/happytoolin/happycontext"
	zerologhc "github.com/happytoolin/happycontext/adapter/zerolog"
	"github.com/rs/zerolog"
)

func TestZerologAdapterWritesStructuredLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	sink := zerologhc.New(&logger)
	if sink == nil {
		t.Fatal("expected sink to be created")
	}

	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1, Message: "zerolog test message"})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "example", "adapter-zerolog", "test", true)
	op.End(nil)

	output := buf.String()
	if !strings.Contains(output, "zerolog test message") {
		t.Error("expected log output to contain 'zerolog test message'")
	}
	if !strings.Contains(output, "adapter-zerolog") {
		t.Error("expected log output to contain 'adapter-zerolog'")
	}
}

func TestZerologAdapterWithNilLogger(t *testing.T) {
	sink := zerologhc.New(nil)
	if sink == nil {
		t.Fatal("expected sink to be created even with nil logger")
	}
	sink.Write(context.Background(), nil)
}

func TestZerologAdapterAllLevels(t *testing.T) {
	levels := []hc.Level{hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError}

	for _, level := range levels {
		var buf bytes.Buffer
		logger := zerolog.New(&buf)
		sink := zerologhc.New(&logger)
		rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1, Message: "level test"})
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		hc.SetLevel(op.Context(), level)
		op.End(nil)

		if buf.Len() == 0 {
			t.Errorf("expected output for level %s", level)
		}
	}
}

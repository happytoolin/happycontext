package main

import (
	"bytes"
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

	fields := map[string]any{
		"example": "adapter-zerolog",
		"test":    true,
	}

	sink.Write(hc.LevelInfo, "zerolog test message", fields)

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

	// Should not panic when writing with nil logger
	sink.Write(hc.LevelInfo, "test", map[string]any{"key": "value"})
}

func TestZerologAdapterAllLevels(t *testing.T) {
	levels := []hc.Level{hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError}

	for _, level := range levels {
		var buf bytes.Buffer
		logger := zerolog.New(&buf)
		sink := zerologhc.New(&logger)

		sink.Write(level, "level test", map[string]any{"level": string(level)})

		output := buf.String()
		if output == "" {
			t.Errorf("expected output for level %s", level)
		}
	}
}

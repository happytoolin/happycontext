package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/happytoolin/unolog"
	uzerolog "github.com/happytoolin/unolog/adapter/zerolog"
	"github.com/rs/zerolog"
)

func TestZerologAdapterWritesStructuredLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	sink := uzerolog.New(&logger)
	if sink == nil {
		t.Fatal("expected sink to be created")
	}

	rt := unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1, Message: "zerolog test message"})
	op := unolog.Start(context.Background(), rt, unolog.OperationStart{Domain: unolog.DomainJob, Name: "t"})
	unolog.Add(op.Context(), "example", "adapter-zerolog", "test", true)
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
	sink := uzerolog.New(nil)
	if sink == nil {
		t.Fatal("expected sink to be created even with nil logger")
	}
	sink.Write(context.Background(), nil)
}

func TestZerologAdapterAllLevels(t *testing.T) {
	levels := []unolog.Level{unolog.LevelDebug, unolog.LevelInfo, unolog.LevelWarn, unolog.LevelError}

	for _, level := range levels {
		var buf bytes.Buffer
		logger := zerolog.New(&buf)
		sink := uzerolog.New(&logger)
		rt := unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1, Message: "level test"})
		op := unolog.Start(context.Background(), rt, unolog.OperationStart{Domain: unolog.DomainJob, Name: "t"})
		unolog.SetLevel(op.Context(), level)
		op.End(nil)

		if buf.Len() == 0 {
			t.Errorf("expected output for level %s", level)
		}
	}
}

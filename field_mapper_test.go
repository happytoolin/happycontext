package hc

import (
	"context"
	"testing"
)

func TestFieldMapperRedactsAndDropsFields(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{
		Domain: DomainHTTP,
		Name:   "request",
	})
	Add(ctx, "password", "secret", "token.raw", "abc123", "debug", "drop-me", "safe", "ok")

	sink := NewTestSink()
	FinishOperation(Config{
		Sink:         sink,
		SamplingRate: 1,
		FieldMapper: ChainFieldMappers(
			RedactKeys("password"),
			RedactKeyPrefixes("token."),
			DropKeys("debug"),
		),
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainHTTP, Name: "request"},
		Code:  200,
	})

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	fields := events[0].Fields
	if fields["password"] != defaultRedactedValue {
		t.Fatalf("password = %v, want redacted", fields["password"])
	}
	if fields["token.raw"] != defaultRedactedValue {
		t.Fatalf("token.raw = %v, want redacted", fields["token.raw"])
	}
	if _, ok := fields["debug"]; ok {
		t.Fatal("debug field was not dropped")
	}
	if fields["safe"] != "ok" {
		t.Fatalf("safe = %v, want ok", fields["safe"])
	}
}

func TestFieldMapperRunsOnlyForKeptEvents(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{
		Domain: DomainHTTP,
		Name:   "request",
	})
	Add(ctx, "field", "value")

	calls := 0
	wrote := FinishOperation(Config{
		Sink:         NewTestSink(),
		SamplingRate: 0,
		FieldMapper: func(key string, value any) (any, bool) {
			calls++
			return value, true
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainHTTP, Name: "request"},
		Code:  200,
	})

	if wrote {
		t.Fatal("FinishOperation wrote a sampled-out healthy event")
	}
	if calls != 0 {
		t.Fatalf("field mapper calls = %d, want 0", calls)
	}
}

func TestEnricherRunsBeforeSampling(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{
		Domain: DomainHTTP,
		Name:   "request",
	})

	sink := NewTestSink()
	wrote := FinishOperation(Config{
		Sink: sink,
		Enrichers: []Enricher{
			func(ctx context.Context, event *Event) {
				AddString(ctx, "tenant", "enterprise")
			},
		},
		Sampler: func(in SampleInput) bool {
			return EventFields(in.Event)["tenant"] == "enterprise"
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainHTTP, Name: "request"},
		Code:  200,
	})

	if !wrote {
		t.Fatal("FinishOperation did not write event kept by enriched sampler")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Fields["tenant"] != "enterprise" {
		t.Fatalf("tenant = %v, want enterprise", events[0].Fields["tenant"])
	}
}

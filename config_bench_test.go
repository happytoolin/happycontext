package hc

import (
	"context"
	"fmt"
	"testing"
)

type benchDiscardSink struct{}

func (benchDiscardSink) Write(Level, string, map[string]any) {}

func BenchmarkFinishOperationWithPolicies(b *testing.B) {
	cfg := NormalizeConfig(Config{
		Sink:         benchDiscardSink{},
		SamplingRate: 1,
		LevelSamplingRates: map[Level]float64{
			LevelDebug: 0.1,
			LevelInfo:  1,
			LevelWarn:  1,
			LevelError: 1,
		},
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {
				SuccessLevel: LevelInfo,
				FailureLevel: LevelError,
				PanicLevel:   LevelError,
				OutcomeLevels: map[Outcome]Level{
					OutcomeTimeout: LevelWarn,
				},
			},
		},
	})

	b.ReportAllocs()
	for b.Loop() {
		op := StartOperation(context.Background(), OperationStart{
			Domain:  DomainJob,
			Name:    "cleanup",
			ID:      "job_1",
			Attempt: 1,
		})
		var err error
		op.End(cfg, &err)
	}
}

func BenchmarkFinishOperationPolicyScale(b *testing.B) {
	for _, count := range []int{1, 16, 128} {
		b.Run(fmt.Sprintf("policies=%d", count), func(b *testing.B) {
			policies := make(map[Domain]OperationPolicy, count)
			for i := range count {
				policies[Domain(fmt.Sprintf("domain-%d", i))] = OperationPolicy{
					SuccessLevel: LevelInfo,
					FailureLevel: LevelError,
					PanicLevel:   LevelError,
				}
			}
			cfg := NormalizeConfig(Config{
				Sink:              benchDiscardSink{},
				SamplingRate:      1,
				OperationPolicies: policies,
			})
			start := OperationStart{Domain: Domain(fmt.Sprintf("domain-%d", count-1)), Name: "benchmark"}

			b.ReportAllocs()
			for b.Loop() {
				op := StartOperation(context.Background(), start)
				var err error
				op.End(cfg, &err)
			}
		})
	}
}

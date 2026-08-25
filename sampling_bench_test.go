package hc

import "testing"

func BenchmarkRateSampler(b *testing.B) {
	sampler := RateSampler(0.5)
	in := SampleInput{Outcome: OutcomeSuccess}

	b.Run("serial", func(b *testing.B) {
		for b.Loop() {
			sampler(in)
		}
	})
	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				sampler(in)
			}
		})
	})
}

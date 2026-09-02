package hc

import (
	"fmt"
	"testing"
	"time"
)

// Allocation gates for the record encode paths, in the shape of slog's
// TestJSONAllocs (log/slog/json_handler_test.go): exact or generous
// allocation budgets enforced in the ordinary test run, not left to
// benchstat discipline. The gated property is the dedupe crossover
// (record.go dedupeScanLimit = 24): events of up to 24 fields must
// encode without any allocation beyond the mandatory line buffer, and
// wider events take the seen-set path (which allocates by design).
// Found as a gap by mutation testing (M10): collapsing the crossover to
// 1 kept every behavior test green — only an alloc gate can see it.

func allocFields(n int) []Field {
	fields := make([]Field, 0, n)
	for i := 0; i < n; i++ {
		fields = append(fields, fieldStr(fmt.Sprintf("k%02d", i), "v"))
	}
	return fields
}

// TestRecordEncodeAllocNarrowPath pins the narrow side of the
// crossover: deduping and encoding 20 fields (one duplicate, so the
// last-wins scan actually runs) allocates zero — the whole point of the
// allocation-free scan. The dst is pre-sized past the encoded line so
// append growth cannot allocate.
func TestRecordEncodeAllocNarrowPath(t *testing.T) {
	fields := allocFields(19)
	fields = append(fields, fieldStr("k00", "last")) // duplicate of the first key
	if len(fields) != 20 {
		t.Fatalf("setup: %d fields, want 20", len(fields))
	}

	dst := make([]byte, 1, 4096)
	dst[0] = '{'
	if allocs := testing.AllocsPerRun(200, func() {
		appendDedupedFields(dst, fields)
	}); allocs != 0 {
		t.Fatalf("narrow path (20 fields) allocated %.0f times, want 0 — "+
			"the allocation-free scan no longer runs (dedupeScanLimit?)", allocs)
	}
}

// TestRecordEncodeAllocWidePath pins the wide side of the crossover:
// 25 fields must take the seen-set path (which allocates by design —
// the kept index slice), and 33 unique fields force the map handoff
// past the 32-slot stack array, which allocates beyond the kept slice.
// The field slices are built OUTSIDE the measured closures: setup
// allocations inside the closure would satisfy the gate vacuously for
// any crossover value (review finding DS-6). Together with
// TestRecordEncodeAllocNarrowPath the boundary sits between 20 and 25.
func TestRecordEncodeAllocWidePath(t *testing.T) {
	fields25 := allocFields(25)
	fields33 := allocFields(33)
	dst := make([]byte, 1, 4096)
	dst[0] = '{'

	allocs25 := testing.AllocsPerRun(200, func() {
		appendDedupedFields(dst, fields25)
	})
	if allocs25 < 1 {
		t.Fatalf("wide path (25 fields) allocated %.0f times, want >= 1 — "+
			"the crossover no longer routes wide events to the seen-set path", allocs25)
	}
	allocs33 := testing.AllocsPerRun(200, func() {
		appendDedupedFields(dst, fields33)
	})
	if allocs33 < 1 {
		t.Fatalf("wide path (33 unique fields) allocated %.0f times, want >= 1 "+
			"(the map handoff must allocate)", allocs33)
	}
	if allocs33 <= allocs25 {
		t.Fatalf("33 unique fields allocated %.0f times, want more than the 25-field "+
			"path (%.0f) — the 32-slot stack array did not hand off to the map", allocs33, allocs25)
	}
}

// TestRecordEncodeAllocFreshRecord is the end-to-end gate with a
// generous ceiling, slog-style: each measured run encodes a fresh
// 20-field record (a fresh *Record per run so the lazy-once publish in
// Encoded cannot mask the encode cost; the RFC3339 second cache is
// primed by AllocsPerRun's warmup call). The healthy baseline is 3
// allocs — the Record struct, the encodedLine node, and the line
// buffer. The ceiling is set wide (6) so ordinary churn never trips
// it, but a regression that falls back to per-field json.Marshal
// (which allocates ~44 for a 20-member map on this toolchain) fails
// loudly.
func TestRecordEncodeAllocFreshRecord(t *testing.T) {
	fields := allocFields(20)
	completedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)
	encode := func() {
		r := &Record{level: LevelInfo, msg: "m", fields: fields, completedAt: completedAt}
		_ = r.Encoded()
	}
	const ceiling = 6 // baseline 3: Record + encodedLine + line buffer
	if allocs := testing.AllocsPerRun(200, encode); allocs > ceiling {
		t.Fatalf("fresh 20-field record encode allocated %.0f times, want <= %d — "+
			"a json.Marshal-style regression added per-field allocations", allocs, ceiling)
	}
}

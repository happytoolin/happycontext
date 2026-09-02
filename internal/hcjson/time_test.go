package hcjson

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestAppendTimeFormats(t *testing.T) {
	cases := []struct {
		t      time.Time
		format string
		want   string
	}{
		{time.Date(2026, 8, 30, 12, 34, 56, 0, time.UTC), time.RFC3339, `"2026-08-30T12:34:56Z"`},
		{time.Date(2026, 8, 30, 12, 34, 56, 0, time.FixedZone("CET", 2*3600)), time.RFC3339, `"2026-08-30T12:34:56+02:00"`},
		{time.Date(2026, 8, 30, 12, 34, 56, 123456789, time.UTC), time.RFC3339, `"2026-08-30T12:34:56Z"`},
		{time.Date(2026, 8, 30, 12, 34, 56, 0, time.UTC), "2006-01-02", `"2026-08-30"`},
	}
	for _, c := range cases {
		if got := string(Encoder{}.AppendTime(nil, c.t, c.format)); got != c.want {
			t.Errorf("AppendTime(%v, %q) = %s, want %s", c.t, c.format, got, c.want)
		}
	}
}

func TestAppendDuration(t *testing.T) {
	e := Encoder{}
	cases := []struct {
		got  string
		want string
	}{
		// zerolog adapter defaults: float milliseconds, shortest precision
		{string(e.AppendDuration(nil, 1500*time.Millisecond, time.Millisecond, false, -1)), "1500"},
		{string(e.AppendDuration(nil, 2500*time.Microsecond, time.Millisecond, false, -1)), "2.5"},
		{string(e.AppendDuration(nil, 0, time.Millisecond, false, -1)), "0"},
		{string(e.AppendDuration(nil, 123456789*time.Nanosecond, time.Millisecond, false, -1)), "123.456789"},
		{string(e.AppendDuration(nil, 1500*time.Millisecond, time.Millisecond, true, -1)), "1500"},
		{string(e.AppendDuration(nil, time.Second, time.Second, false, -1)), "1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("AppendDuration = %s, want %s", c.got, c.want)
		}
	}
}

func TestAppendTimeRFC3339Cached(t *testing.T) {
	// prime the cache with a known local second
	base := time.Now().Truncate(time.Second)
	same := base.Add(900 * time.Millisecond) // same wall second

	a := AppendTimeRFC3339(nil, base)
	b := AppendTimeRFC3339(nil, same)
	if !bytes.Equal(a, b) {
		t.Fatalf("same-second renders differ: %s vs %s", a, b)
	}
	want := string(Encoder{}.AppendTime(nil, base, time.RFC3339))
	if string(a) != want {
		t.Fatalf("cached render %s != Format %s", a, want)
	}

	// next second must re-render
	next := base.Add(time.Second + time.Millisecond)
	c := AppendTimeRFC3339(nil, next)
	wantNext := string(Encoder{}.AppendTime(nil, next, time.RFC3339))
	if string(c) != wantNext {
		t.Fatalf("next-second render %s != %s", c, wantNext)
	}
	if base.Add(time.Second).Format(time.RFC3339) == base.Format(time.RFC3339) {
		t.Fatalf("test clock granularity too coarse to observe rollover")
	}

	// appends into non-empty dst keep the prefix
	dst := AppendTimeRFC3339([]byte(`{"time":`), base)
	if !bytes.HasPrefix(dst, []byte(`{"time":`)) || dst[len(dst)-1] != '"' {
		t.Fatalf("prefix/suffix mangled: %s", dst)
	}
}

func TestAppendTimeRFC3339CachedConcurrent(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				tt := base.Add(time.Duration(j%1000) * time.Millisecond)
				got := string(AppendTimeRFC3339(nil, tt))
				want := `"` + tt.Format(time.RFC3339) + `"`
				if got != want {
					t.Errorf("cached render %s != Format %s", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// FuzzAppendTimeRFC3339 checks the cached formatter continuously
// (dst-research §6.4): for times expressed in the process's local zone
// — the documented cache contract — the output must equal
// time.Format(time.RFC3339) exactly, whatever the second, and must
// parse back to the same wall-clock second. Times from other zones go
// through the documented-caveat path (the cache may reuse the local
// rendering), so for those the oracle is parse-back-to-the-same-second
// only, which is the guarantee downstream parsers rely on.
func FuzzAppendTimeRFC3339(f *testing.F) {
	seeds := []time.Time{
		time.Unix(0, 0),
		time.Unix(-1, 0),
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 1, 10, 0, 0, 999999999, time.UTC),
		time.Date(2026, 9, 1, 10, 0, 0, 1, time.FixedZone("+14", 14*3600)),
		time.Date(2026, 9, 1, 10, 0, 0, 0, time.FixedZone("-12", -12*3600)),
		time.Now(),
	}
	for _, s := range seeds {
		f.Add(s.Unix(), int32(s.Nanosecond()), uint8(0))
	}
	f.Fuzz(func(t *testing.T, sec int64, nsec int32, zoneChoice uint8) {
		// keep years within RFC3339's four-digit range so the parse-back
		// oracle is well-defined for every generated instant — including
		// after conversion to the +14:00 extreme zone (16h headroom)
		const year9999 = 253402300799 // 9999-12-31T23:59:59Z
		if sec < 0 || sec > year9999-16*3600 {
			sec = sec % (year9999 - 16*3600 + 1)
			if sec < 0 {
				sec += year9999 - 16*3600 + 1
			}
		}
		if nsec < 0 {
			nsec = -nsec
		}
		inst := time.Unix(sec, int64(nsec))
		if zoneChoice&1 == 0 {
			local := inst.In(time.Local)
			// the cache is shared process-wide and keyed by Unix second, so a
			// prior iteration in another zone could pollute this second (the
			// documented limitation); force a miss to test the fresh-format
			// path, then call again to pin the cached-hit path.
			rfc3339Cache.Store(&rfc3339Second{sec: -1 << 62})
			got := string(AppendTimeRFC3339(nil, local))
			want := `"` + local.Format(time.RFC3339) + `"`
			if got != want {
				t.Fatalf("local-zone render %s != Format %s (t=%v)", got, want, local)
			}
			if hit := string(AppendTimeRFC3339(nil, local)); hit != want {
				t.Fatalf("cached render %s != Format %s (t=%v)", hit, want, local)
			}
		} else {
			zones := []*time.Location{
				time.UTC,
				time.FixedZone("+14", 14*3600),
				time.FixedZone("-12", -12*3600),
				time.FixedZone("+05:30", 5*3600+1800),
			}
			other := inst.In(zones[int(zoneChoice)%len(zones)])
			got := string(AppendTimeRFC3339(nil, other))
			var s string
			if err := json.Unmarshal([]byte(got), &s); err != nil {
				t.Fatalf("not a valid JSON string: %v (%q)", err, got)
			}
			parsed, err := time.Parse(time.RFC3339, s)
			if err != nil {
				t.Fatalf("rendering %q is not RFC3339: %v", s, err)
			}
			// second precision: whatever rendering the cache served, the
			// parsed instant must be the same second (sub-second dropped)
			if parsed.Unix() != other.Unix() {
				t.Fatalf("parsed second %d != %d (rendering %q)", parsed.Unix(), other.Unix(), s)
			}
		}
	})
}

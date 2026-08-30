package hcjson

import (
	"bytes"
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

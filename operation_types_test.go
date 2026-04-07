package hc

import "testing"

func TestNormalizeDomainDefaultsEmpty(t *testing.T) {
	if got := normalizeDomain(""); got != defaultDomainValue {
		t.Fatalf("normalizeDomain(\"\") = %q, want %q", got, defaultDomainValue)
	}
	if got := normalizeDomain(DomainJob); got != DomainJob {
		t.Fatalf("normalizeDomain(%q) = %q, want %q", DomainJob, got, DomainJob)
	}
}

func TestAsIntAcceptsIntegerWidths(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int
		ok    bool
	}{
		{name: "int", value: int(3), want: 3, ok: true},
		{name: "int16", value: int16(4), want: 4, ok: true},
		{name: "uint8", value: uint8(5), want: 5, ok: true},
		{name: "string", value: "6", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := asInt(tt.value)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("asInt(%T(%v)) = (%d, %v), want (%d, %v)", tt.value, tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}
}

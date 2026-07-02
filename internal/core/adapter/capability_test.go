package adapter

import "testing"

func TestCapsAndHas(t *testing.T) {
	s := Caps(CapExplain, CapJobs)
	if !s.Has(CapExplain) || !s.Has(CapJobs) {
		t.Error("advertised capabilities should report Has == true")
	}
	if s.Has(CapLineage) {
		t.Error("unadvertised capability should report Has == false")
	}
	// The nil/zero set advertises nothing without panicking.
	var zero CapabilitySet
	if zero.Has(CapExplain) {
		t.Error("zero CapabilitySet must advertise nothing")
	}
}

func TestCapabilitySetSorted(t *testing.T) {
	s := Caps(CapSecurity, CapExplain, CapJobs)
	got := s.Sorted()
	if len(got) != 3 {
		t.Fatalf("want 3 caps, got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Sorted() not ascending: %v", got)
		}
	}
}

func TestAllCapabilitiesCoversDefinedConstants(t *testing.T) {
	// Guard against adding a capability constant but forgetting to list it, which
	// would make it invisible to .caps / the MCP capabilities tool.
	want := []Capability{
		CapExplain, CapLineage, CapSource, CapTableFunctions,
		CapJobs, CapSecurity, CapSecurityEdit,
	}
	got := AllCapabilities()
	if len(got) != len(want) {
		t.Fatalf("AllCapabilities len = %d, want %d", len(got), len(want))
	}
	seen := map[Capability]bool{}
	for _, c := range got {
		if seen[c] {
			t.Errorf("duplicate capability %q in AllCapabilities", c)
		}
		seen[c] = true
	}
	for _, c := range want {
		if !seen[c] {
			t.Errorf("capability %q missing from AllCapabilities", c)
		}
	}
}

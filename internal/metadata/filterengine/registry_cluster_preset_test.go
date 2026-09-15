package filterengine

import "testing"

func TestValidClusterFilterPreset(t *testing.T) {
	valid := []ClusterFilterPreset{ClusterFilterOff, ClusterFilterConservative, ClusterFilterBalanced, ClusterFilterAggressive}
	for _, p := range valid {
		if !ValidClusterFilterPreset(p) {
			t.Errorf("ValidClusterFilterPreset(%q) = false, want true", p)
		}
	}
	if ValidClusterFilterPreset("nonsense") {
		t.Error("ValidClusterFilterPreset(\"nonsense\") = true, want false")
	}
}

// TestThresholdForPreset_OffAndUnknownAreZero pins that an unrecognized
// preset degrades to the v1 veto-parity threshold rather than to an
// untested value — see ThresholdForPreset's doc.
func TestThresholdForPreset_OffAndUnknownAreZero(t *testing.T) {
	if got := ThresholdForPreset(ClusterFilterOff); got != 0 {
		t.Errorf("ThresholdForPreset(off) = %v, want 0", got)
	}
	if got := ThresholdForPreset("nonsense"); got != 0 {
		t.Errorf("ThresholdForPreset(nonsense) = %v, want 0", got)
	}
}

func TestThresholdForPreset_KnownPresetsAreNegative(t *testing.T) {
	for _, p := range []ClusterFilterPreset{ClusterFilterConservative, ClusterFilterBalanced, ClusterFilterAggressive} {
		if got := ThresholdForPreset(p); got >= 0 {
			t.Errorf("ThresholdForPreset(%q) = %v, want < 0", p, got)
		}
	}
}

// TestClusterSignalForPreset_OffIsNil pins that callers can distinguish "no
// signal" from "a signal that happens not to fire" — internal/api/authors.go
// must only append a non-nil result.
func TestClusterSignalForPreset_OffIsNil(t *testing.T) {
	if s := ClusterSignalForPreset(ClusterFilterOff); s != nil {
		t.Errorf("ClusterSignalForPreset(off) = %v, want nil", s)
	}
	if s := ClusterSignalForPreset("nonsense"); s != nil {
		t.Errorf("ClusterSignalForPreset(nonsense) = %v, want nil", s)
	}
}

func TestClusterSignalForPreset_KnownPresetsProduceASignal(t *testing.T) {
	for _, p := range []ClusterFilterPreset{ClusterFilterConservative, ClusterFilterBalanced, ClusterFilterAggressive} {
		s := ClusterSignalForPreset(p)
		if s == nil {
			t.Fatalf("ClusterSignalForPreset(%q) = nil, want a signal", p)
		}
		if s.ID() != "cluster.editionCountSupport" {
			t.Errorf("ClusterSignalForPreset(%q).ID() = %q, want cluster.editionCountSupport", p, s.ID())
		}
	}
}

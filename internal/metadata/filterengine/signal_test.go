package filterengine

import (
	"testing"

	"github.com/vavallee/bindery/internal/models"
)

// TestRegistryIsVetoOnlyAtV1 guards the parity default. Migration 086 ships
// keep_threshold = exclude_threshold = 0 for every existing metadata
// profile, which only reproduces the pre-#2235 boolean chain's keep/exclude
// decision while every registered signal is exclude-direction at exactly
// -vetoWeight. Registering a graded (any other magnitude) or keep-direction
// (positive weight) signal in DefaultSignals without also revisiting the
// shipped thresholds is a silent behavior change for every existing user's
// profile, so it fails here instead of shipping quietly.
func TestRegistryIsVetoOnlyAtV1(t *testing.T) {
	for _, s := range DefaultSignals() {
		if w := s.Weight(); w != -vetoWeight {
			t.Errorf("signal %q has weight %v, want exactly %v (v1 registry must be veto-only) — "+
				"if you're intentionally adding a graded or keep-direction signal, migration 086's "+
				"keep_threshold/exclude_threshold defaults must move with it, not stay implicit",
				s.ID(), w, -vetoWeight)
		}
	}
}

func TestNewRegistry_RejectsDuplicateID(t *testing.T) {
	_, err := NewRegistry(NewLanguageSignal(), NewLanguageSignal())
	if err == nil {
		t.Fatal("want an error registering two signals with the same ID, got nil")
	}
}

func TestNewRegistry_RejectsEmptyID(t *testing.T) {
	_, err := NewRegistry(&fakeSignal{id: ""})
	if err == nil {
		t.Fatal("want an error registering a signal with an empty ID, got nil")
	}
}

func TestMustRegistry_PanicsOnDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want MustRegistry to panic on a duplicate ID")
		}
	}()
	MustRegistry(NewLanguageSignal(), NewLanguageSignal())
}

func TestDefaultRegistry_MatchesDefaultSignalsOrder(t *testing.T) {
	r := DefaultRegistry()
	signals := r.Signals()
	want := []string{
		"mediatype.strictMismatch",
		"junk.titleEmptyOrAuthorName",
		"language.notAllowed",
		"structure.partBookTitle",
		"catalog.missingReleaseDate",
		"catalog.missingISBN",
		"catalog.belowMinPages",
	}
	if len(signals) != len(want) {
		t.Fatalf("registry has %d signals, want %d", len(signals), len(want))
	}
	for i, id := range want {
		if signals[i].ID() != id {
			t.Errorf("signal at index %d = %q, want %q (registry order must match the pre-#2235 check order)", i, signals[i].ID(), id)
		}
	}
}

type fakeSignal struct{ id string }

func (f *fakeSignal) ID() string      { return f.id }
func (f *fakeSignal) Weight() float64 { return -vetoWeight }
func (f *fakeSignal) Observe(Candidate, *Context) []models.FilterObservation {
	return nil
}

package models

// FilterObservation is one signal's claim about one candidate work, together
// with the evidence that produced it (#2235). It replaces a bare boolean
// "should this be filtered" with a graded claim: Contribution = Weight *
// Confidence, and a record's score is the sum of its observations'
// contributions plus a prior.
//
// Weight is config/profile-owned magnitude — negative excludes, positive
// keeps. Confidence is 0..1 and is always computed in Go, never configured:
// it is graded logic ("two foreign function words is stronger evidence than
// one"), and exposing it as a second knob would give one degree of freedom
// two separate controls. At v1 every registered signal is a veto
// (Weight == -vetoWeight, Confidence == 1 when it fires), which is what
// makes migration 086's keep_threshold=exclude_threshold=0 default reproduce
// the pre-#2235 boolean-chain behavior exactly — see
// internal/metadata/filterengine's package doc and
// TestRegistryIsVetoOnlyAtV1.
//
// Deliberately four fields wide. The spec this is drawn from
// (fiction-author-dataset-engine, spec/bindery.md) also carries a stored
// Direction string and an untyped evidence map; Direction is dropped here in
// favor of the Direction() method below (a stored copy of sign(Weight) can
// disagree with Weight itself, which golangci-lint won't catch and a
// reviewer will), and the untyped evidence map has no Go equivalent worth an
// `any` — Reason already carries what the UI and audit log need.
type FilterObservation struct {
	// Signal is the emitting signal's stable id, e.g. "language.notAllowed".
	// Namespaced by concept (language./structure./catalog./junk./mediatype.)
	// so a Reason can be traced back to the code that produced it.
	Signal string `json:"signal"`
	// Weight is the signal's configured magnitude. Negative excludes,
	// positive keeps. At v1 every shipped signal uses ±vetoWeight.
	Weight float64 `json:"weight"`
	// Confidence is 0..1, computed by the signal itself from the evidence it
	// saw. A veto-strength signal that fired always reports 1.
	Confidence float64 `json:"confidence"`
	// Reason is a human-readable explanation, meant to reach an audit log or
	// the sync summary's skipped-book sample directly — not a debug string.
	Reason string `json:"reason"`
}

// Contribution is this observation's contribution to a candidate's score:
// Weight * Confidence.
func (o FilterObservation) Contribution() float64 {
	return o.Weight * o.Confidence
}

// Direction reports "keep", "exclude", or "" (a zero-weight observation,
// which no shipped signal currently emits) — derived from Weight's sign
// rather than stored, so it can never disagree with the weight that produced
// it. Exists for audit-log readability; scoring never reads it.
func (o FilterObservation) Direction() string {
	switch {
	case o.Weight > 0:
		return "keep"
	case o.Weight < 0:
		return "exclude"
	default:
		return ""
	}
}

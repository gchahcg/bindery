package models

import "time"

// UnknownLanguageBehavior controls what happens when a metadata source
// returns no language for a book while the profile has a non-empty
// allowedLanguages list. See #232.
const (
	UnknownLanguagePass = "pass"
	UnknownLanguageFail = "fail"
)

type MetadataProfile struct {
	ID                      int64  `json:"id"`
	Name                    string `json:"name"`
	MinPopularity           int    `json:"minPopularity"`
	MinPages                int    `json:"minPages"`
	SkipMissingDate         bool   `json:"skipMissingDate"`
	SkipMissingISBN         bool   `json:"skipMissingIsbn"`
	SkipPartBooks           bool   `json:"skipPartBooks"`
	AllowedLanguages        string `json:"allowedLanguages"`
	UnknownLanguageBehavior string `json:"unknownLanguageBehavior"`
	// KeepThreshold and ExcludeThreshold are internal/metadata/filterengine's
	// banding thresholds (migration 086, #2235). Both default to 0, which —
	// with every v1 signal at veto weight — reproduces the pre-#2235 boolean
	// filter chain's keep/exclude decision exactly. At v1 the API layer
	// (internal/api/metadata_profiles.go) rejects any value where
	// ExcludeThreshold != KeepThreshold: see filterengine's package doc for
	// why the REVIEW band those two disagreeing would open up has nothing to
	// populate it meaningfully yet.
	KeepThreshold    float64   `json:"keepThreshold"`
	ExcludeThreshold float64   `json:"excludeThreshold"`
	CreatedAt        time.Time `json:"createdAt"`
	// OwnerUserID is the per-user ownership column added in migration 025.
	// Zero means "no recorded owner" (legacy pre-backfill rows); auth's
	// CheckOwnership treats that as visible to every authenticated caller.
	OwnerUserID int64 `json:"-"`
}

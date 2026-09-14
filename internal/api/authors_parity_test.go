package api

import (
	"context"
	"testing"
	"time"

	"github.com/vavallee/bindery/internal/db"
	"github.com/vavallee/bindery/internal/metadata"
	"github.com/vavallee/bindery/internal/models"
)

// TestAuthorSyncParity_ShippedDefaultReproducesBooleanChain is the golden
// parity gate for #2235's filterengine wiring into fetchAuthorBooks.
//
// TestAuthorSyncSummaryReconciles (author_sync_reconcile_test.go) already
// exercises every ported signal at once against a catalogue built to trip
// each one, asserting an exact expected count per Skipped* counter — it
// passed unmodified once filterengine replaced the inline boolean checks,
// which is itself strong evidence the wiring didn't shift behavior. This
// test pins the same property at the exact migration-086 shipped default
// (keep_threshold = exclude_threshold = 0) via a separate, smaller fixture,
// as an independent confirmation.
//
// This test does NOT sweep other equal threshold pairs — see
// TestAuthorSyncParity_NonZeroEqualThresholdsDiverge below for why "keep ==
// exclude" alone is not the actual parity invariant, and
// validateScoreThresholds (internal/api/metadata_profiles.go) enforces the
// narrower one this test relies on.
func TestAuthorSyncParity_ShippedDefaultReproducesBooleanChain(t *testing.T) {
	released := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	baseWorks := []models.Book{
		{ForeignID: "OL-keep", Title: "A Real Book", SortTitle: "A Real Book", Language: "eng",
			ReleaseDate: &released, Status: models.BookStatusWanted, MetadataProvider: "openlibrary", Genres: []string{}},
		{ForeignID: "OL-lang", Title: "Un Livre", SortTitle: "Un Livre", Language: "fre",
			ReleaseDate: &released, Status: models.BookStatusWanted, MetadataProvider: "openlibrary", Genres: []string{}},
		{ForeignID: "OL-junk", Title: "Prolix Author", SortTitle: "Prolix Author", Language: "eng",
			ReleaseDate: &released, Status: models.BookStatusWanted, MetadataProvider: "openlibrary", Genres: []string{}},
	}

	runAt := func(t *testing.T, keep, exclude float64) (added, skippedLang, skippedJunk int) {
		t.Helper()
		ctx := context.Background()
		database, err := db.OpenMemory()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Close() })

		authorRepo := db.NewAuthorRepo(database)
		bookRepo := db.NewBookRepo(database)
		profileRepo := db.NewMetadataProfileRepo(database)

		profile := &models.MetadataProfile{
			Name: "Parity", AllowedLanguages: "eng",
			KeepThreshold: keep, ExcludeThreshold: exclude,
		}
		if err := profileRepo.Create(ctx, profile); err != nil {
			t.Fatal(err)
		}
		author := &models.Author{
			ForeignID: "OL-parity", Name: "Prolix Author", SortName: "Author, Prolix",
			MetadataProvider: "openlibrary", MetadataProfileID: &profile.ID,
		}
		if err := authorRepo.Create(ctx, author); err != nil {
			t.Fatal(err)
		}

		provider := &stubMetaProvider{works: baseWorks}
		h := NewAuthorHandler(authorRepo, nil, bookRepo, nil, metadata.NewAggregator(provider), nil, profileRepo, nil)
		h.FetchAuthorBooks(author, false, "")

		sync := h.syncSummaries.get(author.ID)
		if sync == nil {
			t.Fatal("no summary recorded for the sync")
		}
		return sync.Added, sync.SkippedLanguage, sync.SkippedJunk
	}

	added, lang, junk := runAt(t, 0, 0)
	if added != 1 || lang != 1 || junk != 1 {
		t.Errorf("at the shipped 0/0 default: added=%d skippedLanguage=%d skippedJunk=%d, want 1/1/1 "+
			"(1 real book kept, the French book language-excluded, the author-name-titled work junk-excluded)",
			added, lang, junk)
	}
}

// TestAuthorSyncParity_NonZeroEqualThresholdsDiverge documents a real
// correctness finding from building this test suite, not a hypothetical:
// keep_threshold == exclude_threshold alone does NOT reproduce the pre-#2235
// boolean chain. It only does at exactly 0, because every v1 signal is a
// veto with Context.Prior hardcoded to 0 — an unfiltered candidate always
// scores exactly 0, so the shared threshold has to sit exactly there for a
// clean candidate to land KEEP. A nonzero equal pair (this test uses 50/50)
// excludes every candidate, filtered or not, because 0 < 50.
//
// This is exactly why validateScoreThresholds rejects any nonzero value,
// not merely an unequal pair — an earlier version of that function only
// checked ExcludeThreshold != KeepThreshold, which this exact scenario
// caught as broken. This test constructs the profile through the repo
// directly (bypassing the API's validation) specifically to demonstrate
// what that validation exists to prevent, matching the failure mode it
// guards against rather than asserting the guard itself (that's
// TestMetaProfileCreate_RejectsNonZeroEqualThresholds in
// metadata_profiles_test.go).
func TestAuthorSyncParity_NonZeroEqualThresholdsDiverge(t *testing.T) {
	ctx := context.Background()
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	authorRepo := db.NewAuthorRepo(database)
	bookRepo := db.NewBookRepo(database)
	profileRepo := db.NewMetadataProfileRepo(database)

	// Bypasses the API's validateScoreThresholds on purpose (see doc above).
	profile := &models.MetadataProfile{
		Name: "Broken", AllowedLanguages: "eng",
		KeepThreshold: 50, ExcludeThreshold: 50,
	}
	if err := profileRepo.Create(ctx, profile); err != nil {
		t.Fatal(err)
	}
	author := &models.Author{
		ForeignID: "OL-broken", Name: "Real Author", SortName: "Author, Real",
		MetadataProvider: "openlibrary", MetadataProfileID: &profile.ID,
	}
	if err := authorRepo.Create(ctx, author); err != nil {
		t.Fatal(err)
	}

	released := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	provider := &stubMetaProvider{works: []models.Book{
		{ForeignID: "OL-clean", Title: "A Perfectly Ordinary Book", SortTitle: "A Perfectly Ordinary Book",
			Language: "eng", ReleaseDate: &released, Status: models.BookStatusWanted,
			MetadataProvider: "openlibrary", Genres: []string{}},
	}}
	h := NewAuthorHandler(authorRepo, nil, bookRepo, nil, metadata.NewAggregator(provider), nil, profileRepo, nil)
	h.FetchAuthorBooks(author, false, "")

	sync := h.syncSummaries.get(author.ID)
	if sync == nil {
		t.Fatal("no summary recorded for the sync")
	}
	// This IS the divergence: a book with nothing wrong with it — no filter
	// would have touched it under the pre-#2235 boolean chain — is excluded
	// solely because the profile's thresholds are nonzero. added should be 1
	// under correct (0/0) behavior; it is 0 here, which is the point.
	if sync.Added != 0 {
		t.Fatalf("expected the nonzero-threshold bug to reproduce (added=0), got added=%d — "+
			"either the bug this test documents was fixed at the engine level (update this test and its doc) "+
			"or something else changed", sync.Added)
	}
}

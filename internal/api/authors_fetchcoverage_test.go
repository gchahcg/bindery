package api

import (
	"context"
	"errors"
	"testing"

	"github.com/vavallee/bindery/internal/db"
	"github.com/vavallee/bindery/internal/metadata"
	"github.com/vavallee/bindery/internal/models"
)

// errWorksProvider is a stub whose GetAuthorWorks always fails, used to
// exercise fetchAuthorBooks's works-fetch error early-return (authors.go:1861).
type errWorksProvider struct {
	stubMetaProvider
	worksErr error
}

func (p *errWorksProvider) GetAuthorWorks(context.Context, string) ([]models.Book, error) {
	return nil, p.worksErr
}

// errEditionProvider is a stub whose GetEditions fails for specific foreign
// IDs, used to exercise the MinPages/SkipMissingISBN edition prefetch's
// failed-lookup "not enforcing for this work" branch (authors.go:2253).
type errEditionProvider struct {
	stubMetaProvider
	editionErrByBook map[string]error
}

func (p *errEditionProvider) GetEditions(ctx context.Context, fid string) ([]models.Edition, error) {
	if p.editionErrByBook != nil {
		if err, ok := p.editionErrByBook[fid]; ok {
			p.editionCallsMu.Lock()
			p.editionCalls = append(p.editionCalls, fid)
			p.editionCallsMu.Unlock()
			return nil, err
		}
	}
	return p.stubMetaProvider.GetEditions(ctx, fid)
}

// newFetchCoverageFixture builds an in-memory author handler backed by the
// given provider, creates the author, and returns the handler, the created
// author, the book repo, and the metadata-profile repo.
func newFetchCoverageFixture(t *testing.T, provider metadata.Provider, author *models.Author) (*AuthorHandler, *models.Author, *db.BookRepo, *db.MetadataProfileRepo) {
	t.Helper()
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	authorRepo := db.NewAuthorRepo(database)
	bookRepo := db.NewBookRepo(database)
	profileRepo := db.NewMetadataProfileRepo(database)
	ctx := context.Background()
	if err := authorRepo.Create(ctx, author); err != nil {
		t.Fatal(err)
	}
	agg := metadata.NewAggregator(provider)
	h := NewAuthorHandler(authorRepo, nil, bookRepo, nil, agg, nil, profileRepo, nil)
	return h, author, bookRepo, profileRepo
}

// TestFetchAuthorBooksAsync_NilAuthor covers the nil-author guard at the top
// of the async sync entry point (authors.go:699).
func TestFetchAuthorBooksAsync_NilAuthor(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	authorRepo := db.NewAuthorRepo(database)
	bookRepo := db.NewBookRepo(database)
	profileRepo := db.NewMetadataProfileRepo(database)
	h := NewAuthorHandler(authorRepo, nil, bookRepo, nil, metadata.NewAggregator(&stubMetaProvider{}), nil, profileRepo, nil)

	// Must return immediately, without panicking or touching the provider.
	h.fetchAuthorBooksAsync(nil, catalogueSyncOptions{})
}

// TestFetchAuthorBooks_CalibreRelinkFailure covers the early return taken when
// a calibre-linked author cannot be re-linked to a metadata provider
// (authors.go:1830). The stub finds no matching author, so the relink fails
// and the sync stops before importing any books.
func TestFetchAuthorBooks_CalibreRelinkFailure(t *testing.T) {
	author := &models.Author{
		ForeignID:        "calibre:12345",
		Name:             "Calibre Author",
		SortName:         "Author, Calibre",
		MetadataProvider: "openlibrary",
	}
	h, author, bookRepo, _ := newFetchCoverageFixture(t, &stubMetaProvider{}, author)
	ctx := context.Background()

	h.fetchAuthorBooks(ctx, author, catalogueSyncOptions{})

	got, err := h.authors.GetByID(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForeignID != "calibre:12345" {
		t.Errorf("ForeignID = %q, want unchanged calibre:12345", got.ForeignID)
	}
	books, err := bookRepo.ListByAuthor(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 0 {
		t.Errorf("expected no books after a failed relink, got %d", len(books))
	}
}

// TestFetchAuthorBooks_WorksProviderError covers the early return taken when
// the provider's author-works fetch fails (authors.go:1861).
func TestFetchAuthorBooks_WorksProviderError(t *testing.T) {
	author := &models.Author{
		ForeignID:        "OL999A",
		Name:             "Broken Author",
		SortName:         "Author, Broken",
		MetadataProvider: "openlibrary",
	}
	stub := &errWorksProvider{worksErr: errors.New("works fetch failed")}
	h, author, bookRepo, _ := newFetchCoverageFixture(t, stub, author)
	ctx := context.Background()

	h.fetchAuthorBooks(ctx, author, catalogueSyncOptions{})

	books, err := bookRepo.ListByAuthor(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 0 {
		t.Errorf("expected no books after a failed works fetch, got %d", len(books))
	}
}

// TestFetchAuthorBooks_EditionLookupFailure covers the edition prefetch's
// failed-lookup branch (authors.go:2253): when a metadata profile enforces
// MinPages and a candidate's edition lookup errors, the work is still created
// rather than silently dropped.
func TestFetchAuthorBooks_EditionLookupFailure(t *testing.T) {
	author := &models.Author{
		ForeignID:        "OLEA",
		Name:             "Edition Author",
		SortName:         "Author, Edition",
		MetadataProvider: "openlibrary",
	}
	stub := &errEditionProvider{
		stubMetaProvider: stubMetaProvider{
			works: []models.Book{
				{ForeignID: "OLW1", Title: "Min Pages Book", SortTitle: "min pages book", Status: models.BookStatusWanted, Genres: []string{}, MetadataProvider: "openlibrary"},
			},
		},
		editionErrByBook: map[string]error{"OLW1": errors.New("edition lookup failed")},
	}
	h, author, bookRepo, profileRepo := newFetchCoverageFixture(t, stub, author)

	// Enable a MinPages rule on the default profile so the sync prefetches
	// editions (needsEditionPreview), which is what exposes the failure branch.
	ctx := context.Background()
	profile, err := profileRepo.GetByID(ctx, models.DefaultMetadataProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.MinPages = 100
	if err := profileRepo.Update(ctx, profile); err != nil {
		t.Fatal(err)
	}

	h.fetchAuthorBooks(ctx, author, catalogueSyncOptions{})

	books, err := bookRepo.ListByAuthor(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("expected the work to be created despite the failed edition lookup, got %d books", len(books))
	}
	stub.editionCallsMu.Lock()
	calls := len(stub.editionCalls)
	stub.editionCallsMu.Unlock()
	if calls != 1 {
		t.Errorf("expected exactly one GetEditions attempt, got %d", calls)
	}
}

// TestFetchAuthorBooks_SeriesModeMonitorsPinnedSeries covers the series-mode
// monitored-series load (authors.go:2055-2072) and the monitor-on-discovery
// short-circuit (authors.go:2356-2362): a series-mode author with a pinned
// series gets a freshly discovered work flipped to monitored at creation time
// when the provider's SeriesRefs already name that series.
func TestFetchAuthorBooks_SeriesModeMonitorsPinnedSeries(t *testing.T) {
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	authorRepo := db.NewAuthorRepo(database)
	bookRepo := db.NewBookRepo(database)
	seriesRepo := db.NewSeriesRepo(database)
	profileRepo := db.NewMetadataProfileRepo(database)
	ctx := context.Background()

	author := &models.Author{
		ForeignID:        "OLSERA",
		Name:             "Series Author",
		SortName:         "Author, Series",
		MetadataProvider: "openlibrary",
		Monitored:        true,
		MonitorMode:      models.AuthorMonitorModeSeries,
	}
	if err := authorRepo.Create(ctx, author); err != nil {
		t.Fatal(err)
	}

	// A pinned series carrying a foreign ID.
	series := &models.Series{ForeignID: "OLSER1", Title: "Dune Chronicles"}
	if err := seriesRepo.Create(ctx, series); err != nil {
		t.Fatal(err)
	}

	// A pre-existing library book in that series, so ListByAuthor resolves the
	// series for this author (the series_books join in authors.go:2060).
	existing := &models.Book{
		ForeignID:        "OLX1",
		AuthorID:         author.ID,
		Title:            "Dune",
		SortTitle:        "dune",
		Status:           models.BookStatusWanted,
		Genres:           []string{},
		MetadataProvider: "openlibrary",
	}
	if err := bookRepo.Create(ctx, existing); err != nil {
		t.Fatal(err)
	}
	if err := seriesRepo.LinkBook(ctx, series.ID, existing.ID, "1", true); err != nil {
		t.Fatal(err)
	}
	if err := authorRepo.SetMonitoredSeriesIDs(ctx, author.ID, []int64{series.ID}); err != nil {
		t.Fatal(err)
	}

	// Provider returns a NEW work that names the pinned series.
	stub := &stubMetaProvider{
		works: []models.Book{{
			ForeignID:        "OLN1",
			Title:            "Dune Messiah",
			SortTitle:        "dune messiah",
			Status:           models.BookStatusWanted,
			Genres:           []string{},
			MetadataProvider: "openlibrary",
			SeriesRefs:       []models.SeriesRef{{ForeignID: "OLSER1", Title: "Dune Chronicles"}},
		}},
	}
	h := NewAuthorHandler(authorRepo, nil, bookRepo, seriesRepo, metadata.NewAggregator(stub), nil, profileRepo, nil)

	h.fetchAuthorBooks(ctx, author, catalogueSyncOptions{})

	books, err := bookRepo.ListByAuthor(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("expected 2 books (pre-existing + discovered), got %d: %+v", len(books), books)
	}
	for _, b := range books {
		if b.Title == "Dune Messiah" && !b.Monitored {
			t.Errorf("discovered book in a pinned series should be monitored, got Monitored=%v", b.Monitored)
		}
	}
}

package importer

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vavallee/bindery/internal/models"
)

// opfProbe is a namespace-agnostic parse-back of the fields BuildOPFXML
// writes, mirroring the style parseOPFMetadata uses to read a real EPUB's
// embedded OPF (epubmeta.go) — proving the output is not just
// string-shaped but genuinely well-formed, parseable XML.
type opfProbe struct {
	XMLName  xml.Name `xml:"package"`
	UniqueID string   `xml:"unique-identifier,attr"`
	Metadata struct {
		Title       string   `xml:"title"`
		Creator     string   `xml:"creator"`
		Contributor string   `xml:"contributor"`
		Language    string   `xml:"language"`
		Publisher   string   `xml:"publisher"`
		Date        string   `xml:"date"`
		Description string   `xml:"description"`
		Subjects    []string `xml:"subject"`
		Identifiers []struct {
			ID     string `xml:"id,attr"`
			Scheme string `xml:"scheme,attr"`
			Value  string `xml:",chardata"`
		} `xml:"identifier"`
		Meta []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
}

func parseOPFProbe(t *testing.T, xmlBytes []byte) opfProbe {
	t.Helper()
	var p opfProbe
	if err := xml.Unmarshal(xmlBytes, &p); err != nil {
		t.Fatalf("BuildOPFXML output did not parse as XML: %v\n---\n%s", err, xmlBytes)
	}
	return p
}

func metaContent(p opfProbe, name string) (string, bool) {
	for _, m := range p.Metadata.Meta {
		if m.Name == name {
			return m.Content, true
		}
	}
	return "", false
}

func fullBookFixture() (*models.Book, *models.Author, *models.Edition) {
	release := time.Date(2020, 3, 15, 0, 0, 0, 0, time.UTC)
	pub := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	isbn13 := "9780345472199"
	book := &models.Book{
		ID:               42,
		Title:            "The Way of Kings",
		SortTitle:        "Way of Kings, The",
		Description:      "A stormlight epic.",
		Genres:           []string{"Fantasy", "Epic"},
		ReleaseDate:      &release,
		Language:         "eng",
		Narrator:         "Michael Kramer",
		ASIN:             "B0031RS9YE",
		ForeignID:        "12345",
		MetadataProvider: "hardcover",
		AverageRating:    4.7,
		RatingsCount:     9001,
	}
	author := &models.Author{
		Name:     "Brandon Sanderson",
		SortName: "Sanderson, Brandon",
	}
	edition := &models.Edition{
		BookID:      42,
		ISBN13:      &isbn13,
		Publisher:   "Tor Books",
		PublishDate: &pub,
		Language:    "eng",
	}
	return book, author, edition
}

func TestBuildOPFXML_FullFields(t *testing.T) {
	book, author, edition := fullBookFixture()

	xmlBytes, err := BuildOPFXML(book, author, edition, "The Stormlight Archive", "1")
	if err != nil {
		t.Fatalf("BuildOPFXML: %v", err)
	}
	p := parseOPFProbe(t, xmlBytes)

	if p.Metadata.Title != book.Title {
		t.Errorf("title = %q, want %q", p.Metadata.Title, book.Title)
	}
	if p.Metadata.Creator != author.Name {
		t.Errorf("creator = %q, want %q", p.Metadata.Creator, author.Name)
	}
	if p.Metadata.Contributor != book.Narrator {
		t.Errorf("contributor (narrator) = %q, want %q", p.Metadata.Contributor, book.Narrator)
	}
	if p.Metadata.Language != "en" {
		t.Errorf("language = %q, want normalized %q", p.Metadata.Language, "en")
	}
	if p.Metadata.Publisher != "Tor Books" {
		t.Errorf("publisher = %q, want %q", p.Metadata.Publisher, "Tor Books")
	}
	if p.Metadata.Date != "2021-06-01" {
		t.Errorf("date = %q, want edition.PublishDate %q", p.Metadata.Date, "2021-06-01")
	}
	if p.Metadata.Description != book.Description {
		t.Errorf("description = %q, want %q", p.Metadata.Description, book.Description)
	}
	if len(p.Metadata.Subjects) != 2 || p.Metadata.Subjects[0] != "Fantasy" || p.Metadata.Subjects[1] != "Epic" {
		t.Errorf("subjects = %v, want [Fantasy Epic]", p.Metadata.Subjects)
	}
	if series, ok := metaContent(p, "calibre:series"); !ok || series != "The Stormlight Archive" {
		t.Errorf("calibre:series = %q (found=%v), want %q", series, ok, "The Stormlight Archive")
	}
	if idx, ok := metaContent(p, "calibre:series_index"); !ok || idx != "1" {
		t.Errorf("calibre:series_index = %q (found=%v), want %q", idx, ok, "1")
	}
	if sortTitle, ok := metaContent(p, "calibre:title_sort"); !ok || sortTitle != book.SortTitle {
		t.Errorf("calibre:title_sort = %q (found=%v), want %q", sortTitle, ok, book.SortTitle)
	}

	// Identifiers: bindery id is the unique-identifier target; ISBN and ASIN
	// use their special-cased Calibre scheme names.
	found := map[string]struct{ scheme, id string }{}
	for _, ident := range p.Metadata.Identifiers {
		found[ident.Value] = struct{ scheme, id string }{ident.Scheme, ident.ID}
	}
	if got := found["42"]; got.scheme != "BINDERY" || got.id != "bindery-id" {
		t.Errorf("bindery identifier = %+v, want scheme BINDERY id bindery-id", got)
	}
	if p.UniqueID != "bindery-id" {
		t.Errorf("package unique-identifier = %q, want %q", p.UniqueID, "bindery-id")
	}
	if got := found["9780345472199"]; got.scheme != "ISBN" {
		t.Errorf("isbn identifier scheme = %q, want ISBN", got.scheme)
	}
	if got := found["B0031RS9YE"]; got.scheme != "MOBI-ASIN" {
		t.Errorf("asin identifier scheme = %q, want MOBI-ASIN", got.scheme)
	}
}

// TestBuildOPFXML_ExcludesRatings is the field-scope assertion this feature
// hinges on: docs/third-party-data.md draws the exact same line for
// commercial-use compliance ("Title, author, series, edition, publisher,
// narrator, description, and cover are facts and are fine" vs.
// "average_rating and ratings_count ... [s]trip them"). The sidecar must
// never carry AverageRating/RatingsCount so a deployment that already
// excludes them from the API/UI per that doc doesn't have them leak back
// out through this file instead.
func TestBuildOPFXML_ExcludesRatings(t *testing.T) {
	book, author, edition := fullBookFixture()
	xmlBytes, err := BuildOPFXML(book, author, edition, "", "")
	if err != nil {
		t.Fatalf("BuildOPFXML: %v", err)
	}
	out := string(xmlBytes)
	for _, needle := range []string{"4.7", "9001", "rating"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(needle)) {
			t.Errorf("output must not reference AverageRating/RatingsCount, found %q in:\n%s", needle, out)
		}
	}
}

func TestBuildOPFXML_NoCoverReference(t *testing.T) {
	book, author, edition := fullBookFixture()
	book.ImageURL = "https://example.com/cover.jpg"
	xmlBytes, err := BuildOPFXML(book, author, edition, "", "")
	if err != nil {
		t.Fatalf("BuildOPFXML: %v", err)
	}
	if strings.Contains(string(xmlBytes), "cover") {
		t.Errorf("output must not reference a cover (none is ever written to the library folder), got:\n%s", xmlBytes)
	}
}

func TestBuildOPFXML_MinimalBook(t *testing.T) {
	book := &models.Book{ID: 7, Title: "Bare Book"}
	xmlBytes, err := BuildOPFXML(book, nil, nil, "", "")
	if err != nil {
		t.Fatalf("BuildOPFXML: %v", err)
	}
	p := parseOPFProbe(t, xmlBytes)
	if p.Metadata.Title != "Bare Book" {
		t.Errorf("title = %q, want %q", p.Metadata.Title, "Bare Book")
	}
	if p.Metadata.Creator != "" {
		t.Errorf("creator = %q, want empty (nil author)", p.Metadata.Creator)
	}
	if len(p.Metadata.Identifiers) != 1 || p.Metadata.Identifiers[0].Value != "7" {
		t.Errorf("identifiers = %+v, want just the bindery id", p.Metadata.Identifiers)
	}
}

func TestBuildOPFXML_NilBook(t *testing.T) {
	if _, err := BuildOPFXML(nil, nil, nil, "", ""); err == nil {
		t.Fatal("expected an error for a nil book, got nil")
	}
}

// TestBuildOPFXML_EscapesSpecialCharacters guards against a title/author
// containing XML metacharacters (an ampersand is routine — "Fantasy &
// Science Fiction") producing malformed output a reader can't parse.
func TestBuildOPFXML_EscapesSpecialCharacters(t *testing.T) {
	book := &models.Book{ID: 1, Title: `<Weird> & "Title"`}
	author := &models.Author{Name: "A & B", SortName: `B, "A"`}
	xmlBytes, err := BuildOPFXML(book, author, nil, "", "")
	if err != nil {
		t.Fatalf("BuildOPFXML: %v", err)
	}
	p := parseOPFProbe(t, xmlBytes)
	if p.Metadata.Title != book.Title {
		t.Errorf("round-tripped title = %q, want %q", p.Metadata.Title, book.Title)
	}
	if p.Metadata.Creator != author.Name {
		t.Errorf("round-tripped creator = %q, want %q", p.Metadata.Creator, author.Name)
	}
}

func TestWriteOPFSidecarFile(t *testing.T) {
	dir := t.TempDir()
	bookDir := filepath.Join(dir, "Brandon Sanderson", "The Way of Kings (2020)")
	book, author, edition := fullBookFixture()

	if err := WriteOPFSidecarFile(bookDir, book, author, edition, "", ""); err != nil {
		t.Fatalf("WriteOPFSidecarFile: %v", err)
	}
	dest := filepath.Join(bookDir, "metadata.opf")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", dest, err)
	}
	p := parseOPFProbe(t, data)
	if p.Metadata.Title != book.Title {
		t.Errorf("written file title = %q, want %q", p.Metadata.Title, book.Title)
	}

	// Re-writing (e.g. via Reorganize) must overwrite cleanly, not append or
	// fail because the file and directory already exist.
	book.Title = "The Way of Kings: Author's Definitive Edition"
	if err := WriteOPFSidecarFile(bookDir, book, author, edition, "", ""); err != nil {
		t.Fatalf("WriteOPFSidecarFile (overwrite): %v", err)
	}
	data, err = os.ReadFile(dest)
	if err != nil {
		t.Fatalf("re-read after overwrite: %v", err)
	}
	p = parseOPFProbe(t, data)
	if p.Metadata.Title != book.Title {
		t.Errorf("after overwrite, title = %q, want %q", p.Metadata.Title, book.Title)
	}
}

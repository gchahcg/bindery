package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vavallee/bindery/internal/auth"
	"github.com/vavallee/bindery/internal/db"
	"github.com/vavallee/bindery/internal/models"
)

func metaProfileFixture(t *testing.T) (*MetadataProfileHandler, *db.MetadataProfileRepo, context.Context) {
	t.Helper()
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	repo := db.NewMetadataProfileRepo(database)
	return NewMetadataProfileHandler(repo), repo, context.Background()
}

// TestMetaProfileCreate_DefaultsAllowedLanguages — #14 regression guard.
// When the client omits allowedLanguages, we default to "eng" rather than
// persisting an empty string that would let non-English editions slip
// through the author-refresh filter.
func TestMetaProfileCreate_DefaultsAllowedLanguages(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Default"}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got models.MetadataProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AllowedLanguages != "eng" {
		t.Errorf("expected default allowedLanguages=eng, got %q", got.AllowedLanguages)
	}
}

// TestMetaProfileCreate_RejectsInvertedThresholds and
// TestMetaProfileCreate_RejectsNonEqualThresholds are the regression tests
// for validateScoreThresholds (#2235, migration 086). At v1 both
// keep_threshold and exclude_threshold must be exactly 0 — not merely equal
// to each other, see TestMetaProfileCreate_RejectsNonZeroEqualThresholds
// below and validateScoreThresholds's own doc for why "equal" alone isn't
// the actual rule.
func TestMetaProfileCreate_RejectsInvertedThresholds(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Inverted","keepThreshold":-10,"excludeThreshold":10}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for exclude > keep, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMetaProfileCreate_RejectsNonEqualThresholds(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Graded","keepThreshold":10,"excludeThreshold":-10}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a nonzero threshold pair at v1, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMetaProfileCreate_RejectsNonZeroEqualThresholds pins the corrected
// (tightened) validateScoreThresholds rule: equal is not enough, both must
// be exactly 0 at v1. See TestAuthorSyncParity_NonZeroEqualThresholdsDiverge
// (authors_parity_test.go) for the end-to-end scenario that caught the
// original (equal-only) version of this check as insufficient — a
// keep=exclude=50 profile passed that check yet excluded every candidate,
// filtered or not.
func TestMetaProfileCreate_RejectsNonZeroEqualThresholds(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"StillBroken","keepThreshold":50,"excludeThreshold":50}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a nonzero equal threshold pair, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMetaProfileCreate_AcceptsEqualThresholds(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Parity","keepThreshold":0,"excludeThreshold":0}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 for keep == exclude, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMetaProfileUpdate_RejectsNonEqualThresholds(t *testing.T) {
	h, repo, ctx := metaProfileFixture(t)
	p := &models.MetadataProfile{Name: "Original", AllowedLanguages: "eng"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"name":"Original","keepThreshold":5,"excludeThreshold":0}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), body)
	req = withURLParam(req, "id", strconv.FormatInt(p.ID, 10))
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a nonzero keepThreshold on update, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMetaProfileCreate_RequiresName(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"allowedLanguages":"eng"}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestMetaProfileCreate_BadBody(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", bytes.NewBufferString("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestMetaProfileList_EmptyIsArray(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata-profile", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if bytes.TrimSpace(rec.Body.Bytes())[0] != '[' {
		t.Errorf("expected JSON array, got %s", rec.Body.String())
	}
}

func TestMetaProfileGet_NotFound(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/v1/metadata-profile/999", nil), "id", "999")
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestMetaProfileUpdate_RoundTrip(t *testing.T) {
	h, repo, ctx := metaProfileFixture(t)
	p := &models.MetadataProfile{Name: "Fiction", AllowedLanguages: "eng"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"name":"Fiction","allowedLanguages":"eng,fre","minPages":100}`)
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), body), "id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := repo.GetByID(ctx, p.ID)
	if got.AllowedLanguages != "eng,fre" || got.MinPages != 100 {
		t.Errorf("update did not persist, got %+v", got)
	}
}

func TestMetaProfileUpdate_NotFound(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/999", bytes.NewBufferString(`{"name":"X"}`)), "id", "999")
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestMetaProfileDelete_Success(t *testing.T) {
	h, repo, ctx := metaProfileFixture(t)
	p := &models.MetadataProfile{Name: "X", AllowedLanguages: "eng"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), nil), "id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

// TestMetaProfileCreate_RejectsInvalidClusterFilterPreset is the #2235 Phase 2
// regression guard for validateClusterFilterPreset: a client can only pick a
// preset from the closed set (off/conservative/balanced/aggressive), never an
// arbitrary keep/exclude pair. An unknown value is rejected on create.
func TestMetaProfileCreate_RejectsInvalidClusterFilterPreset(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Cluster","clusterFilterPreset":"bogus"}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown clusterFilterPreset, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "clusterFilterPreset") {
		t.Errorf("expected the error to name clusterFilterPreset, got %s", rec.Body.String())
	}
}

// TestMetaProfileUpdate_RejectsInvalidClusterFilterPreset is the update-path
// twin of the create guard above: the closed-set check runs on update too, so
// an existing profile cannot be flipped to an unmeasured preset.
func TestMetaProfileUpdate_RejectsInvalidClusterFilterPreset(t *testing.T) {
	h, repo, ctx := metaProfileFixture(t)
	p := &models.MetadataProfile{Name: "Original", AllowedLanguages: "eng"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"name":"Original","clusterFilterPreset":"bogus"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), body)
	req = withURLParam(req, "id", strconv.FormatInt(p.ID, 10))
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown clusterFilterPreset on update, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMetaProfileCreate_AcceptsValidClusterFilterPreset confirms a measured
// preset is accepted and persisted verbatim.
func TestMetaProfileCreate_AcceptsValidClusterFilterPreset(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Cluster","clusterFilterPreset":"balanced"}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got models.MetadataProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ClusterFilterPreset != "balanced" {
		t.Errorf("expected clusterFilterPreset=balanced, got %q", got.ClusterFilterPreset)
	}
}

// TestMetaProfileCreate_DefaultsClusterFilterPresetToOff pins the create-path
// default: omitting the field stores "off" (the signal disabled), matching the
// pre-Phase-2 behaviour for every existing profile.
func TestMetaProfileCreate_DefaultsClusterFilterPresetToOff(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	body := bytes.NewBufferString(`{"name":"Default"}`)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/metadata-profile", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got models.MetadataProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ClusterFilterPreset != "off" {
		t.Errorf("expected default clusterFilterPreset=off, got %q", got.ClusterFilterPreset)
	}
}

// TestMetaProfileUpdate_NonOwnerForbidden is the #2235 Phase 2 IDOR guard
// regression for the Update path (the Get and Delete twins are already
// covered): with the tenancy gate on, a second user must get a 404 — not a
// 200/400/500 that would leak the row's existence — when updating a profile
// owned by someone else.
func TestMetaProfileUpdate_NonOwnerForbidden(t *testing.T) {
	auth.SetEnforceTenancyForTests(t, true)
	database, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	repo := db.NewMetadataProfileRepo(database)
	users := db.NewUserRepo(database)
	u1, err := users.Create(ctx, "alice", "h1")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := users.Create(ctx, "bob", "h2")
	if err != nil {
		t.Fatal(err)
	}
	p := &models.MetadataProfile{Name: "Alice's", AllowedLanguages: "eng"}
	if err := repo.CreateForUser(ctx, p, u1.ID); err != nil {
		t.Fatal(err)
	}
	h := NewMetadataProfileHandler(repo)
	req := newRequestForID(http.MethodPut, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), p.ID, withAuthCtx(context.Background(), u2.ID, "user"))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user Update must 404 with gate on; got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestMetaProfileGet_InvalidID, TestMetaProfileUpdate_InvalidID, and
// TestMetaProfileDelete_InvalidID pin the id-parsing guard on all three
// by-id routes: a non-numeric {id} is a client error (400), not a 404 that
// would conflate "bad input" with "no such row".
func TestMetaProfileGet_InvalidID(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/v1/metadata-profile/abc", nil), "id", "abc")
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-numeric id on Get, got %d", rec.Code)
	}
}

func TestMetaProfileUpdate_InvalidID(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/abc", bytes.NewBufferString(`{"name":"X"}`)), "id", "abc")
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-numeric id on Update, got %d", rec.Code)
	}
}

func TestMetaProfileDelete_InvalidID(t *testing.T) {
	h, _, _ := metaProfileFixture(t)
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/api/v1/metadata-profile/abc", nil), "id", "abc")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-numeric id on Delete, got %d", rec.Code)
	}
}

// TestMetaProfileUpdate_BadBody pins the decode-error guard on Update: an
// unparseable body is a 400, reached only after the id and ownership checks
// pass (so an unowned row with a malformed body still surfaces the 400).
func TestMetaProfileUpdate_BadBody(t *testing.T) {
	h, repo, ctx := metaProfileFixture(t)
	p := &models.MetadataProfile{Name: "X", AllowedLanguages: "eng"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/v1/metadata-profile/"+strconv.FormatInt(p.ID, 10), bytes.NewBufferString("not-json")), "id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a malformed Update body, got %d", rec.Code)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vavallee/bindery/internal/auth"
	"github.com/vavallee/bindery/internal/db"
	"github.com/vavallee/bindery/internal/models"
)

type MetadataProfileHandler struct {
	repo *db.MetadataProfileRepo
}

func NewMetadataProfileHandler(repo *db.MetadataProfileRepo) *MetadataProfileHandler {
	return &MetadataProfileHandler{repo: repo}
}

func (h *MetadataProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.repo.List(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if profiles == nil {
		profiles = []models.MetadataProfile{}
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (h *MetadataProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	p, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "metadata profile not found"})
		return
	}
	// Tier-1 cross-user IDOR guard (D1).
	if !auth.CheckOwnership(r.Context(), p.OwnerUserID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "metadata profile not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *MetadataProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p models.MetadataProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if p.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	if p.AllowedLanguages == "" {
		p.AllowedLanguages = "eng"
	}
	if p.UnknownLanguageBehavior != models.UnknownLanguageFail {
		p.UnknownLanguageBehavior = models.UnknownLanguagePass
	}
	if msg, ok := validateScoreThresholds(p); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if err := h.repo.CreateForUser(r.Context(), &p, auth.UserIDFromContext(r.Context())); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *MetadataProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "metadata profile not found"})
		return
	}
	// Tier-1 cross-user IDOR guard (D1).
	if !auth.CheckOwnership(r.Context(), existing.OwnerUserID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "metadata profile not found"})
		return
	}
	var p models.MetadataProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	p.ID = id
	if p.UnknownLanguageBehavior != models.UnknownLanguageFail {
		p.UnknownLanguageBehavior = models.UnknownLanguagePass
	}
	if msg, ok := validateScoreThresholds(p); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if err := h.repo.Update(r.Context(), &p); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *MetadataProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	// Tier-1 cross-user IDOR guard (D1). Pre-fetch so non-owners cannot
	// observe a 200 / 500 vs. 404 difference and probe for existence.
	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if existing == nil || !auth.CheckOwnership(r.Context(), existing.OwnerUserID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "metadata profile not found"})
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeServerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateScoreThresholds enforces internal/metadata/filterengine's v1
// constraint on a profile's KeepThreshold/ExcludeThreshold pair (#2235,
// migration 086). An inverted band (exclude above keep) is meaningless in
// any version of this scheme, so it's always rejected. At v1 specifically,
// exclude != keep is rejected too — not because a wider band is unsafe, but
// because nothing populates it meaningfully yet: every shipped signal is a
// veto (see filterengine.TestRegistryIsVetoOnlyAtV1), so a REVIEW band would
// exist with nothing graded enough to land a candidate inside it, and there
// is no UI surface to show one on if it did. Relaxing this to `exclude <=
// keep` is the one-line change that turns on graded filtering once a real
// graded signal (the cluster-level edition-count signal cluster.go's doc
// describes) is proven in — deliberately left as a one-line follow-up
// rather than done speculatively here.
func validateScoreThresholds(p models.MetadataProfile) (msg string, ok bool) {
	if p.ExcludeThreshold > p.KeepThreshold {
		return "excludeThreshold cannot be greater than keepThreshold", false
	}
	if p.ExcludeThreshold != p.KeepThreshold {
		return "excludeThreshold must equal keepThreshold; graded filtering (a non-empty review band) isn't enabled yet", false
	}
	return "", true
}

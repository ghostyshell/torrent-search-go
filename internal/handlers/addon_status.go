package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	"torrent-search-go/pkg/models"
	store "torrent-search-go/pkg/storage"
)

// Addon status report handlers.
// Public GETs (GetAddonStatusReports / GetAddonStatusReport) are registered bare in main.go
// (no dashboard-auth wrapper). Create / Replace / Delete are dashboard-password-gated.

// GetAddonStatusReports lists every managed addon status report (public).
func (h *MonitoringHandler) GetAddonStatusReports(w http.ResponseWriter, r *http.Request) {
	reports, err := h.storage.ListAddonStatusReports(r.Context())
	if err != nil {
		writeError(w, "Failed to list addon status reports", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "reports": reports})
}

// GetAddonStatusReport returns one report by addon id (public; 404 if missing).
func (h *MonitoringHandler) GetAddonStatusReport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("addonId"))
	if id == "" {
		writeError(w, "addonId is required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}
	rep, err := h.storage.GetAddonStatusReport(r.Context(), id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			writeError(w, "Addon status report not found", "NOT_FOUND", http.StatusNotFound)
			return
		}
		writeError(w, "Failed to fetch addon status report", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	// When a changelog source URL is set, parse the remote CHANGELOG.md on the
	// backend (cached) and serve that as the changelog; fall back to the stored
	// manual entries on any fetch/parse error so the page never breaks.
	if rep.ChangelogSourceURL != "" {
		if parsed, ferr := fetchChangelogFromURL(r.Context(), rep.ChangelogSourceURL); ferr == nil && len(parsed) > 0 {
			rep.Changelog = parsed
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "report": rep})
}

// CreateAddonStatusReport creates a new report (gated; 409 if the addon id already exists).
func (h *MonitoringHandler) CreateAddonStatusReport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB cap; the seed report is ~3 KB
	var rep models.AddonStatusReport
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		writeError(w, "Invalid JSON body", "BAD_BODY", http.StatusBadRequest)
		return
	}
	if err := normalizeAddonStatusReport(&rep); err != nil {
		writeError(w, err.Error(), "VALIDATION", http.StatusBadRequest)
		return
	}
	existing, err := h.storage.GetAddonStatusReport(r.Context(), rep.Addon.ID)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		writeError(w, "Failed to check existing report", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		writeError(w, "Addon status report already exists; use PUT to update", "ALREADY_EXISTS", http.StatusConflict)
		return
	}
	if err := h.storage.UpsertAddonStatusReport(r.Context(), rep); err != nil {
		writeError(w, "Failed to create addon status report", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": rep.Addon.ID})
}

// ReplaceAddonStatusReport replaces (or creates) a report for the given addon id (gated).
func (h *MonitoringHandler) ReplaceAddonStatusReport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("addonId"))
	if id == "" {
		writeError(w, "addonId is required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rep models.AddonStatusReport
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		writeError(w, "Invalid JSON body", "BAD_BODY", http.StatusBadRequest)
		return
	}
	rep.Addon.ID = id // path wins over body
	if err := normalizeAddonStatusReport(&rep); err != nil {
		writeError(w, err.Error(), "VALIDATION", http.StatusBadRequest)
		return
	}
	if err := h.storage.UpsertAddonStatusReport(r.Context(), rep); err != nil {
		writeError(w, "Failed to save addon status report", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": rep.Addon.ID})
}

// DeleteAddonStatusReport removes a report by addon id (gated; 404 if not found).
func (h *MonitoringHandler) DeleteAddonStatusReport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("addonId"))
	if id == "" {
		writeError(w, "addonId is required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}
	if err := h.storage.DeleteAddonStatusReport(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrAddonStatusNotFound) {
			writeError(w, "Addon status report not found", "NOT_FOUND", http.StatusNotFound)
			return
		}
		writeError(w, "Failed to delete addon status report", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

// normalizeAddonStatusReport trims, validates, and defaults the report fields in place.
func normalizeAddonStatusReport(r *models.AddonStatusReport) error {
	r.Addon.ID = strings.TrimSpace(r.Addon.ID)
	r.Addon.Name = strings.TrimSpace(r.Addon.Name)
	r.Addon.Status = strings.ToUpper(strings.TrimSpace(r.Addon.Status))
	r.Addon.UpdatedAt = strings.TrimSpace(r.Addon.UpdatedAt)
	r.ChangelogSourceURL = strings.TrimSpace(r.ChangelogSourceURL)
	if err := validateChangelogURL(r.ChangelogSourceURL); err != nil {
		return err
	}
	if r.Addon.ID == "" {
		return errors.New("addon.id is required")
	}
	if r.Addon.Name == "" {
		return errors.New("addon.name is required")
	}
	switch r.Addon.Status {
	case "LIVE", "DOWN", "MAINTENANCE":
	default:
		return errors.New("addon.status must be LIVE, DOWN, or MAINTENANCE")
	}
	if r.Addon.UpdatedAt == "" {
		r.Addon.UpdatedAt = time.Now().UTC().Format("2006-01-02")
	}
	for i := range r.Sources {
		s := &r.Sources[i]
		s.ID = strings.TrimSpace(s.ID)
		s.Name = strings.TrimSpace(s.Name)
		s.Note = strings.TrimSpace(s.Note)
		s.Status = strings.ToUpper(strings.TrimSpace(s.Status))
		s.Detail = strings.TrimSpace(s.Detail)
		switch s.Status {
		case "LIVE", "DOWN", "MAINTENANCE":
		default:
			return errors.New("source status must be LIVE, DOWN, or MAINTENANCE")
		}
	}
	for i := range r.Issues {
		it := &r.Issues[i]
		it.ID = strings.TrimSpace(it.ID)
		it.Title = strings.TrimSpace(it.Title)
		it.Status = strings.TrimSpace(it.Status)
		it.Summary = strings.TrimSpace(it.Summary)
		it.UpdatedAt = strings.TrimSpace(it.UpdatedAt)
	}
	for i := range r.Changelog {
		c := &r.Changelog[i]
		c.Version = strings.TrimSpace(c.Version)
		c.Date = strings.TrimSpace(c.Date)
		var hl []string
		for _, h := range c.Highlights {
			if t := strings.TrimSpace(h); t != "" {
				hl = append(hl, t)
			}
		}
		c.Highlights = hl
	}
	return nil
}
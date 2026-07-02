package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	stripchatAPIBase     = "https://stripchat.com"
	stripchatCDNBase     = "https://static-cdn.strpst.com"
	stripchatHTTPTimeout = 8 * time.Second
	stripchatUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	stripchatPageStep   = 30
	stripchatMaxPages   = 6
	// Stripchat returns multiple blocks; Blocks[0] is geo-local (e.g. girls/german).
	stripchatCatalogBlockID = "mostPopularModels"
)

// stripchatPrimaryTag maps a Stripchat catalog id to the primaryTag query
// param used by /api/front/v2/models. "men" (not "guys") is what the API
// expects for the sc_guys catalog.
var stripchatPrimaryTag = map[string]string{
	"sc_girls":   "girls",
	"sc_couples": "couples",
	"sc_guys":    "men",
	"sc_trans":   "trans",
}

// stripchatModel is the subset of the public model object we read.
type stripchatModel struct {
	Username     string `json:"username"`
	Status       string `json:"status"`
	IsLive       bool   `json:"isLive"`
	IsMobile     bool   `json:"isMobile"`
	ViewersCount int    `json:"viewersCount"`
	Country      string `json:"country"`
	AvatarURL    string `json:"avatarUrl"`
	// Snapshot is the live cam still; prefer it, fall back to thumb/avatar below.
	Snapshot             string `json:"snapshot"`
	PreviewURLThumbSmall string `json:"previewUrlThumbSmall"`
	Topic                string `json:"topic"`
}

type stripchatModelsResponse struct {
	Blocks []struct {
		ID     string           `json:"id"`
		Models []stripchatModel `json:"models"`
	} `json:"blocks"`
}

// stripchatHttpClient lets tests stub the HTTP client. nil falls back to the
// default timeout-bound client.
type stripchatHttpClient interface {
	Do(*http.Request) (*http.Response, error)
}

var stripchatClient stripchatHttpClient = &http.Client{Timeout: stripchatHTTPTimeout}

func stripchatStripNil(s string) string { return strings.TrimSpace(s) }

// stripchatAbsMediaURL turns Stripchat API media paths into absolute CDN URLs.
// Stremio ignores relative poster/background URLs in catalog responses.
func stripchatAbsMediaURL(path string) string {
	path = stripchatStripNil(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return stripchatCDNBase + path
}

// stripchatLivePreviewPath turns a thumb-small preview path into the full live
// capture Stripchat serves on the CDN (same hash, without the -thumb-small suffix).
func stripchatLivePreviewPath(path string) string {
	path = stripchatStripNil(path)
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, "-thumb-small")
}

// stripchatPoster picks the best live-stream thumbnail Stremio can render.
// Profile avatars are intentionally excluded; broadcasts/meta paths enrich from
// the models listing when preview fields are missing.
func stripchatPoster(m stripchatModel) string {
	for _, c := range []string{m.Snapshot, stripchatLivePreviewPath(m.PreviewURLThumbSmall)} {
		if u := stripchatAbsMediaURL(c); u != "" {
			return u
		}
	}
	return ""
}

func stripchatDescription(m stripchatModel) string {
	parts := make([]string, 0, 3)
	if m.IsLive {
		parts = append(parts, "live")
	} else if m.Status != "" {
		parts = append(parts, m.Status)
	}
	if m.ViewersCount > 0 {
		parts = append(parts, fmt.Sprintf("%d viewers", m.ViewersCount))
	}
	if stripchatStripNil(m.Country) != "" {
		parts = append(parts, strings.ToUpper(m.Country))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " - ")
}

// stripchatFetchModels fetches one page of the live listing for a primaryTag.
// limit/offset mirror the public API pagination; sorted by viewers desc on the
// server side too, but we re-sort to be safe.
func (h *Handler) stripchatFetchModels(ctx context.Context, primaryTag string, limit, offset int) ([]stripchatModel, error) {
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("primaryTag", primaryTag)
	reqURL := stripchatAPIBase + "/api/front/v2/models?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", stripchatUserAgent)
	req.Header.Set("Referer", stripchatAPIBase+"/")

	res, err := stripchatClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("stripchat models: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var payload stripchatModelsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Blocks) == 0 {
		return nil, nil
	}
	for _, block := range payload.Blocks {
		if block.ID == stripchatCatalogBlockID {
			return block.Models, nil
		}
	}
	return payload.Blocks[0].Models, nil
}

// stripchatFetchCam fetches /api/front/v1/broadcasts/{u}. Returns nil, nil
// when the user does not exist (404); the search/meta paths treat that as
// "no entry" rather than an error. Offline performers may also 404 here;
// ponytail: no users-API fallback yet, search for offline names is best-effort.
func (h *Handler) stripchatFetchCam(ctx context.Context, username string) (*stripchatModel, error) {
	username = stripchatStripNil(username)
	if username == "" {
		return nil, nil
	}
	reqURL := stripchatAPIBase + "/api/front/v1/broadcasts/" + url.PathEscape(username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", stripchatUserAgent)
	req.Header.Set("Referer", stripchatAPIBase+"/"+url.PathEscape(username))

	res, err := stripchatClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("stripchat broadcast: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Item stripchatModel `json:"item"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	m := payload.Item
	if m.Username == "" {
		m.Username = username
	}
	return &m, nil
}

// stripchatLookupModel scans live model listings for an exact username match so
// meta/search can attach a live preview when broadcasts omit preview fields.
// ponytail: up to 3 pages x 4 tags; upgrade path is a dedicated username API if
// Stripchat exposes one.
func (h *Handler) stripchatLookupModel(ctx context.Context, username string) (*stripchatModel, error) {
	want := strings.ToLower(stripchatStripNil(username))
	if want == "" {
		return nil, nil
	}
	tags := []string{"girls", "couples", "men", "trans"}
	for _, tag := range tags {
		for page := 0; page < 3; page++ {
			batch, err := h.stripchatFetchModels(ctx, tag, stripchatPageStep, page*stripchatPageStep)
			if err != nil {
				break
			}
			for i := range batch {
				if strings.ToLower(batch[i].Username) == want {
					m := batch[i]
					return &m, nil
				}
			}
			if len(batch) < stripchatPageStep {
				break
			}
		}
	}
	return nil, nil
}

func (h *Handler) stripchatEnrichPreview(ctx context.Context, m *stripchatModel) {
	if m == nil || stripchatPoster(*m) != "" {
		return
	}
	listing, err := h.stripchatLookupModel(ctx, m.Username)
	if err != nil || listing == nil {
		return
	}
	if listing.PreviewURLThumbSmall != "" {
		m.PreviewURLThumbSmall = listing.PreviewURLThumbSmall
	}
	if listing.Snapshot != "" {
		m.Snapshot = listing.Snapshot
	}
}

func stripchatModelToPreview(m stripchatModel) MetaPreview {
	name := stripchatStripNil(m.Username)
	if name == "" {
		name = "Stripchat"
	}
	return MetaPreview{
		ID:          "sc:" + m.Username,
		Type:        "Porn",
		Name:        name,
		Poster:      stripchatPoster(m),
		Background:  stripchatPoster(m),
		Description: stripchatDescription(m),
		PosterShape: "landscape",
	}
}

// stripchatLoadListing fetches enough pages to fill skip+maxResults live models,
// dedupes by username, filters to public+live, and sorts by viewers descending.
// The result is cached 30s so repeated scrolls don't hit the API.
func (h *Handler) stripchatLoadListing(ctx context.Context, primaryTag string, skip, maxResults int) ([]MetaPreview, error) {
	store := newRedisStore(h.Redis)
	cacheKey := prefixStripchatCatalog + primaryTag + "|" + itoa(skip) + "|" + itoa(maxResults)
	if store != nil {
		if cached, err := store.getStripchatCatalog(ctx, cacheKey); err == nil && len(cached) > 0 {
			return cached, nil
		}
	}

	if maxResults <= 0 {
		maxResults = 20
	}
	need := skip + maxResults
	pages := (need + stripchatPageStep - 1) / stripchatPageStep
	if pages < 1 {
		pages = 1
	}
	if pages > stripchatMaxPages {
		pages = stripchatMaxPages
	}

	seen := make(map[string]struct{})
	models := make([]stripchatModel, 0, pages*stripchatPageStep)
	for page := 0; page < pages; page++ {
		batch, err := h.stripchatFetchModels(ctx, primaryTag, stripchatPageStep, page*stripchatPageStep)
		if err != nil {
			break
		}
		for _, m := range batch {
			if stripchatStripNil(m.Username) == "" {
				continue
			}
			if _, dup := seen[m.Username]; dup {
				continue
			}
			seen[m.Username] = struct{}{}
			models = append(models, m)
		}
		if len(batch) < stripchatPageStep {
			break
		}
	}

	models = stripchatFilterPublicLive(models)
	stripchatSortCatalogModels(models)

	end := skip + maxResults
	if skip > len(models) {
		return []MetaPreview{}, nil
	}
	if end > len(models) {
		end = len(models)
	}
	page := models[skip:end]

	out := make([]MetaPreview, 0, len(page))
	for _, m := range page {
		out = append(out, stripchatModelToPreview(m))
	}
	if store != nil && len(out) > 0 {
		_ = store.setStripchatCatalog(ctx, cacheKey, out)
	}
	return out, nil
}

// stripchatFilterPublicLive drops offline / private models. ponytail: in-place
// filter over the caller's backing slice; safe because every caller discards
// the input immediately.
func stripchatFilterPublicLive(models []stripchatModel) []stripchatModel {
	out := models[:0]
	for _, m := range models {
		if !strings.EqualFold(m.Status, "public") {
			continue
		}
		out = append(out, m)
	}
	return out
}

// stripchatSortCatalogModels ranks global popular models: desktop streams first,
// then viewers descending within each group.
func stripchatSortCatalogModels(models []stripchatModel) {
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].IsMobile != models[j].IsMobile {
			return !models[i].IsMobile
		}
		return models[i].ViewersCount > models[j].ViewersCount
	})
}

func (h *Handler) serveStripchatCatalog(ctx context.Context, catalogID, searchQ string, skip, maxResults int) (CatalogResponse, error) {
	primaryTag, ok := stripchatPrimaryTag[catalogID]
	if !ok {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if maxResults <= 0 {
		maxResults = 20
	}

	if q := stripchatStripNil(searchQ); q != "" {
		// Search resolves one username exactly. Emit a single entry if the user
		// exists (regardless of live state) so an offline performer's page is
		// still reachable - the stream route will return [] when offline.
		m, err := h.stripchatFetchCam(ctx, q)
		if err != nil || m == nil {
			return CatalogResponse{Metas: []MetaPreview{}}, err
		}
		h.stripchatEnrichPreview(ctx, m)
		return CatalogResponse{Metas: []MetaPreview{stripchatModelToPreview(*m)}}, nil
	}

	metas, err := h.stripchatLoadListing(ctx, primaryTag, skip, maxResults)
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	return CatalogResponse{Metas: metas}, nil
}

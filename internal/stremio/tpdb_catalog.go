package stremio

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"torrent-search-go/internal/services/metadata"
)

// serveTPDBCatalog fetches a TPDB catalog page directly from the ThePornDB API.
// catalogID is "tpdb_new" (browse) or "tpdb_search" (keyword search).
func (h *Handler) serveTPDBCatalog(ctx context.Context, catalogID, searchQ string, skip int) (CatalogResponse, error) {
	tpdb := h.tpdbClient()
	if tpdb == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	store := newRedisStore(h.Redis)
	cacheKey := prefixTPDBCatalog + catalogID + "|" + searchQ + "|" + itoa(skip)
	if store != nil {
		if cached, _ := store.getProxiedMetas(ctx, cacheKey); len(cached) > 0 {
			return CatalogResponse{Metas: cached}, nil
		}
	}

	const perPage = 36
	page := skip/perPage + 1

	var items []map[string]interface{}
	var err error
	if searchQ != "" {
		items, err = tpdb.SearchScenesRaw(ctx, searchQ, perPage)
	} else {
		items, err = tpdb.BrowseScenes(ctx, page, perPage)
	}
	if err != nil || len(items) == 0 {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}

	metas := make([]MetaPreview, 0, len(items))
	for _, item := range items {
		id := tpdbSceneID(item)
		if id == "" {
			continue
		}
		name := strMetaVal(item["title"])
		if name == "" {
			continue
		}
		poster := metadata.FindImage(item, "poster")
		bg := metadata.FindImage(item, "background")
		if bg == "" {
			bg = poster
		}
		cardArt := bg
		year := ""
		if d := strMetaVal(item["date"]); len(d) >= 4 {
			year = d[:4]
		}
		// Catalog is declared type "Porn"; items must match meta type or Stremio
		// rejects the detail page as "No metadata found" (same as hentai/series).
		// Use the wide background as card art and landscape shape (TPDB posters
		// are portrait thumbnails).
		metas = append(metas, MetaPreview{
			ID:          id,
			Type:        "Porn",
			Name:        name,
			Poster:      cardArt,
			Background:  bg,
			ReleaseInfo: year,
			PosterShape: "landscape",
		})
	}

	if store != nil && len(metas) > 0 {
		_ = store.setTPDBMetas(ctx, cacheKey, metas)
	}
	return CatalogResponse{Metas: metas}, nil
}

// serveTPDBMeta fetches single-scene metadata from ThePornDB.
// id is the item ID as returned by the catalog, e.g. "porndb:11093443".
func (h *Handler) serveTPDBMeta(ctx context.Context, id string) (*Meta, error) {
	tpdb := h.tpdbClient()
	if tpdb == nil {
		return nil, nil
	}
	numID := extractPornDBNumericID(id)
	if numID == "" {
		return nil, nil
	}

	item, err := tpdb.GetScene(ctx, numID)
	if err != nil || item == nil {
		return nil, err
	}

	name := strMetaVal(item["title"])
	if name == "" {
		return nil, nil
	}
	poster := metadata.FindImage(item, "poster")
	bg := metadata.FindImage(item, "background")
	if bg == "" {
		bg = poster
	}
	year := ""
	if d := strMetaVal(item["date"]); len(d) >= 4 {
		year = d[:4]
	}
	desc := strMetaVal(item["description"])
	if desc == "" {
		desc = strMetaVal(item["summary"])
	}

	var cast []string
	if perfs, ok := item["performers"].([]interface{}); ok {
		for _, p := range perfs {
			if m, ok := p.(map[string]interface{}); ok {
				if n := strMetaVal(m["name"]); n != "" {
					cast = append(cast, n)
				}
			}
		}
	}

	var genres []string
	if tags, ok := item["tags"].([]interface{}); ok {
		for _, t := range tags {
			if m, ok := t.(map[string]interface{}); ok {
				if n := strMetaVal(m["tag"]); n != "" {
					genres = append(genres, n)
				}
			}
		}
	}

	var links []Link
	for _, p := range cast {
		links = append(links, Link{
			Name:     p,
			Category: "Cast",
			URL:      "stremio:///search?search=" + strings.ReplaceAll(p, " ", "+"),
		})
	}
	for _, g := range genres {
		links = append(links, Link{
			Name:     g,
			Category: "Genres",
			URL:      "stremio:///search?search=" + strings.ReplaceAll(g, " ", "+"),
		})
	}

	return &Meta{
		ID:          id,
		Type:        "Porn",
		Name:        name,
		Poster:      bg,
		Background:  bg,
		Description: desc,
		ReleaseInfo: year,
		Genres:      genres,
		Links:       links,
		Website:     strMetaVal(item["url"]),
		PosterShape: "landscape",
	}, nil
}

// ServeStremioStream handles /stremio/:config/stream/:type/:streamFile requests.
// For porndb:{id} items it searches PornRips and returns infoHash streams.
// All other IDs return an empty stream list.
func (h *Handler) ServeStremioStream(w http.ResponseWriter, r *http.Request) {
	streamFile := r.PathValue("streamFile")
	id := strings.TrimSuffix(streamFile, ".json")

	if isHentaiID(id) {
		streams, _ := h.serveProxiedStream(r.Context(), id)
		if streams == nil {
			streams = []map[string]interface{}{}
		}
		writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
		return
	}

	if !strings.HasPrefix(id, "porndb:") {
		writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": []interface{}{}})
		return
	}

	streams := h.tpdbStreams(r.Context(), id)
	if streams == nil {
		streams = []map[string]interface{}{}
	}
	writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
}

func (h *Handler) tpdbStreams(ctx context.Context, id string) []map[string]interface{} {
	if h.Scrapers == nil {
		return nil
	}
	tpdb := h.tpdbClient()
	if tpdb == nil {
		return nil
	}
	numID := extractPornDBNumericID(id)
	if numID == "" {
		return nil
	}
	item, err := tpdb.GetScene(ctx, numID)
	if err != nil || item == nil {
		return nil
	}

	query := tpdbPornRipsQuery(item)
	if query == "" {
		return nil
	}

	// Use the PornRips WordPress REST API (not the HTML scraper) to find PRT
	// releases by performer name. The HTML scraper is blocked by Cloudflare
	// from cloud IPs; the WP REST API is accessible and returns the WP post
	// title which IS the PRT release name (dotted, e.g. Studio.YY.MM.DD..PRT).
	if h.Reference == nil {
		return nil
	}
	refItems, err := h.Reference.FetchPornripsCatalog(ctx, "search", query, 0)
	if err != nil || len(refItems) == 0 {
		return nil
	}
	const maxFetch = 3
	if len(refItems) > maxFetch {
		refItems = refItems[:maxFetch]
	}

	type result struct {
		hash  string
		title string
	}
	results := make([]result, len(refItems))
	var wg sync.WaitGroup
	for i, it := range refItems {
		if it.Meta == nil || it.Meta.Name == "" {
			continue
		}
		wg.Add(1)
		go func(i int, prtName, detailURL string) {
			defer wg.Done()
			data, err := h.Scrapers.FetchTorrentData(ctx, "pornrips", detailURL, prtName)
			if err != nil || len(data) == 0 {
				return
			}
			if hash := infoHashFromTorrent(data); hash != "" {
				results[i] = result{hash: hash, title: prtName}
			}
		}(i, it.Meta.Name, "https://pornrips.to/"+it.Slug+"/")
	}
	wg.Wait()

	out := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		if r.hash == "" {
			continue
		}
		out = append(out, map[string]interface{}{
			"infoHash":      r.hash,
			"name":          "PRT",
			"title":         r.title,
			"behaviorHints": map[string]interface{}{"notWebReady": true},
		})
	}
	return out
}

// tpdbPornRipsQuery builds the best PornRips search query for a TPDB scene.
// PRT filenames always contain performer names; performer-only search covers
// all studios (not just the TPDB scene's studio) and avoids false negatives
// when the studio name doesn't appear in the PRT torrent title.
func tpdbPornRipsQuery(item map[string]interface{}) string {
	if perfs, ok := item["performers"].([]interface{}); ok && len(perfs) > 0 {
		if m, ok := perfs[0].(map[string]interface{}); ok {
			if name := strMetaVal(m["name"]); name != "" {
				return name
			}
		}
	}
	return strMetaVal(item["title"])
}

// tpdbClient creates a TPDB client from the Handler environment, or returns nil.
func (h *Handler) tpdbClient() *metadata.TPDBClient {
	if h.Env == nil || h.Env.Metadata.TPDBAPIKey == "" {
		return nil
	}
	return metadata.NewTPDBClient(h.Env.Metadata.TPDBAPIURL, h.Env.Metadata.TPDBAPIKey)
}

// tpdbSceneID returns the "porndb:{n}" ID for a raw TPDB scene map.
func tpdbSceneID(item map[string]interface{}) string {
	switch v := item["id"].(type) {
	case float64:
		return fmt.Sprintf("porndb:%d", int(v))
	case string:
		if v != "" {
			return "porndb:" + v
		}
	}
	return ""
}

// extractPornDBNumericID strips the "porndb:" prefix.
// e.g. "porndb:11093443" → "11093443".
func extractPornDBNumericID(id string) string {
	return strings.TrimPrefix(id, "porndb:")
}

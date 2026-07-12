package stremio

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/pkg/models"
)

// matchedSourcesFilter returns the torrent-source names the user has configured,
// to gate the enriched_scenes store query ({matched_sources: {$in: ...}}). This
// is the source-config gate the prior live path lacked (it cross-referenced
// pornrips regardless of cfg.Sources): a scene surfaces only when one of the
// user's configured torrent sources resolved it. piratebay/pornrips come from
// cfg.Sources; the extra indexers (knaben_adult/xxxclub/mypornclub)
// from cfg.ExtraIndexers; 1337x from cfg.Enable1337x. sukebei is
// excluded - it is hentai, not porn scenes, and is not in the enriched_scenes
// source-set.
func matchedSourcesFilter(cfg Config) []string {
	var out []string
	for _, s := range cfg.Sources {
		switch s {
		case "piratebay", "pornrips":
			out = append(out, s)
		}
	}
	if cfg.ExtraIndexers {
		out = append(out, "knaben_adult", "xxxclub", "mypornclub")
	}
	if cfg.Enable1337x {
		out = append(out, "1337x")
	}
	return out
}

// sourceEnabled reports whether a torrent source is in cfg.Sources (the
// cfg.Sources list, not the extra-indexer flags). Backs the pornrips fallback
// gate in serveTPDBSearchLocal.
func sourceEnabled(cfg Config, name string) bool {
	for _, s := range cfg.Sources {
		if s == name {
			return true
		}
	}
	return false
}

// enrichedScenesToMetas renders store scenes as Stremio catalog MetaPreviews.
// Catalog is type "Porn"; TPDB/Stash posters are wide scene stills, so the card
// art is the background and the poster shape is landscape (same as the live
// path). Date -> ReleaseInfo year.
func enrichedScenesToMetas(scenes []models.EnrichedScene) []MetaPreview {
	metas := make([]MetaPreview, 0, len(scenes))
	for _, s := range scenes {
		if s.ID == "" || s.Title == "" {
			continue
		}
		bg := s.Background
		if bg == "" {
			bg = s.Poster
		}
		year := ""
		if len(s.Date) >= 4 {
			year = s.Date[:4]
		}
		metas = append(metas, MetaPreview{
			ID:          s.ID,
			Type:        "Porn",
			Name:        s.Title,
			Poster:      bg,
			Background:  bg,
			Description: s.Description,
			ReleaseInfo: year,
			PosterShape: "landscape",
		})
	}
	return metas
}

// enrichedSceneToMeta renders a store scene as a Stremio Meta detail. Cast ->
// Cast links, tags -> Genres links (stremio-core requires Link.Category).
func enrichedSceneToMeta(s *models.EnrichedScene) *Meta {
	bg := s.Background
	if bg == "" {
		bg = s.Poster
	}
	year := ""
	if len(s.Date) >= 4 {
		year = s.Date[:4]
	}
	return &Meta{
		ID:          s.ID,
		Type:        "Porn",
		Name:        s.Title,
		Poster:      bg,
		Background:  bg,
		Description: s.Description,
		ReleaseInfo: year,
		Genres:      s.Tags,
		Links:       metaLinks(s.Cast, s.Tags),
		PosterShape: "landscape",
	}
}

// metaLinks builds Cast + Genres search links. stremio-core rejects a Link with
// an empty Category, so every link is tagged.
func metaLinks(cast, genres []string) []Link {
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
	return links
}

// sceneStreams emits one infoHash stream per matched source the user has
// configured, piratebay preferred over pornrips (higher-quality scrape). The
// store holds one best torrent per source; only sources in `gates` with a
// resolved TorrentRef emit. Stream name labels the source (PRT for pornrips,
// Torrent otherwise).
func sceneStreams(scene *models.EnrichedScene, gates []string) []map[string]interface{} {
	if scene == nil || len(scene.Torrents) == 0 || len(gates) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(gates))
	for _, g := range gates {
		allowed[g] = true
	}
	// Stable source order: piratebay first, then the rest as stored.
	order := make([]string, 0, len(scene.Torrents))
	if ref, ok := scene.Torrents["piratebay"]; ok && allowed["piratebay"] && ref.InfoHash != "" {
		order = append(order, "piratebay")
	}
	for src := range scene.Torrents {
		if src == "piratebay" || !allowed[src] {
			continue
		}
		if scene.Torrents[src].InfoHash == "" {
			continue
		}
		order = append(order, src)
	}
	out := make([]map[string]interface{}, 0, len(order))
	for _, src := range order {
		ref := scene.Torrents[src]
		name := "Torrent"
		if src == "pornrips" {
			name = "PRT"
		}
		title := ref.Title
		if title == "" {
			title = scene.Title
		}
		if title == "" {
			title = "Unknown"
		}
		out = append(out, map[string]interface{}{
			"infoHash":      strings.ToLower(strings.TrimSpace(ref.InfoHash)),
			"name":          name,
			"title":         title,
			"behaviorHints": map[string]interface{}{"notWebReady": true},
		})
	}
	return out
}

// serveTPDBCatalog serves the tpdb_new (browse) and tpdb_search catalogs.
// tpdb_new is store-backed: a {source:"tpdb", matched_sources $in: cfgSources}
// query against enriched_scenes, so a scene surfaces only when one of the user's
// configured torrent sources resolved it (the source-config gate the prior live
// path lacked - it cross-referenced pornrips regardless of cfg.Sources). tpdb_search
// stays live (SearchScenesRaw) and falls back to the pornrips_entries store when
// TPDB has no scene for the query; the on-demand upsert populates enriched_scenes
// from a successful search (see jobs path). Cold store (no EnrichedScenes wired)
// -> tpdb_new returns empty, tpdb_search still works live.
func (h *Handler) serveTPDBCatalog(ctx context.Context, cfg Config, contentType, catalogID, searchQ string, skip int) (CatalogResponse, error) {
	if searchQ == "" {
		return h.serveTPDBBrowse(ctx, cfg, skip)
	}
	return h.serveTPDBSearch(ctx, cfg, contentType, searchQ, skip)
}

// serveTPDBBrowse is the store-backed tpdb_new browse: newest TPDB scenes the
// user's configured sources resolved. Redis-fronted catalog cache keyed by
// skip + the configured source set (sourcesKey) so two installs with different
// cfg.Sources never share one cached page. Empty on a cold store
// (EnrichedScenes nil) - no live fallback, by design (the bulk fill runs before
// the catalog reads flip). The match tick busts these keys when it resolves new
// torrents (matchEnrichedScenes) so a freshly-resolved scene surfaces promptly
// instead of waiting out the 15min TTL.
func (h *Handler) serveTPDBBrowse(ctx context.Context, cfg Config, skip int) (CatalogResponse, error) {
	sources := matchedSourcesFilter(cfg)
	if len(sources) == 0 {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	store := newRedisStore(h.Redis)
	cacheKey := prefixTPDBCatalog + "tpdb_new||" + itoa(skip) + "||" + sourcesKey(sources)
	if store != nil {
		if cached, _ := store.getProxiedMetas(ctx, cacheKey); len(cached) > 0 {
			return CatalogResponse{Metas: cached}, nil
		}
	}
	if h.EnrichedScenes == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	const perPage = 36
	scenes, err := h.EnrichedScenes.GetEnrichedScenesByMatchedSources(ctx, "tpdb", nil, sources, skip, perPage)
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	metas := enrichedScenesToMetas(scenes)
	if store != nil && len(metas) > 0 {
		_ = store.setTPDBMetas(ctx, cacheKey, metas)
	}
	return CatalogResponse{Metas: metas}, nil
}

// serveTPDBSearch is the live tpdb_search: TPDB SearchScenesRaw first, fall back to
// the pornrips_entries store (pr_search pipeline) when TPDB has no scene. The live
// TPDB branch is source-gated via the enriched_scenes store: search items are
// cross-referenced by {matched_sources $in cfg.Sources, _id $in ids} so only scenes a
// configured torrent source already resolved surface - the same gate the store-backed
// tpdb_new browse applies. No live scraping on the request path (a cross-ref is a cheap
// Mongo $in); cold items - not yet matched in the store - are enriched async
// (fire-and-forget, bounded) so the next search surfaces them. Filtered metas are
// Redis-cached keyed by query+sources+skip; empty results are not cached so a cold
// search re-probes once the async enrich + background sweep populate matched_sources.
// Cold install (no EnrichedScenes store) degrades to the prior unfiltered live metas
// under a legacy cache key so it cannot pollute the source-gated cache. The pornrips
// fallback only runs when the user's cfg.Sources actually contains pornrips (the
// gate-bug fix: the old path fell back to pornrips regardless of cfg.Sources).
func (h *Handler) serveTPDBSearch(ctx context.Context, cfg Config, contentType, searchQ string, skip int) (CatalogResponse, error) {
	sources := matchedSourcesFilter(cfg)
	store := newRedisStore(h.Redis)
	cacheKey := prefixTPDBCatalog + "tpdb_search|" + searchQ + "|" + itoa(skip) + "|" + sourcesKey(sources)
	if store != nil {
		if cached, _ := store.getProxiedMetas(ctx, cacheKey); len(cached) > 0 {
			return CatalogResponse{Metas: cached}, nil
		}
	}
	const perPage = 36
	if tpdb := h.tpdbClient(); tpdb != nil {
		items, err := tpdb.SearchScenesRaw(ctx, searchQ, perPage)
		if err == nil && len(items) > 0 {
			// Cold install (no store): degrade to the prior unfiltered live metas,
			// cached under the legacy key (no sources suffix) so the unfiltered
			// result cannot pollute the source-gated cache once the store is wired.
			if h.EnrichedScenes == nil {
				coldKey := prefixTPDBCatalog + "tpdb_search|" + searchQ + "|" + itoa(skip)
				resp, _ := h.tpdbMetasFromItems(ctx, store, coldKey, items, nil)
				return resp, nil
			}
			ids := make([]string, 0, len(items))
			for _, item := range items {
				if id := tpdbSceneID(item); id != "" {
					ids = append(ids, id)
				}
			}
			// Source-gate: cross-ref the store for matched scenes among the search
			// items. No live scraping on the request path - the background
			// EnrichedScenesSync sweep + the async on-demand enrich below populate
			// matched_sources, so a scene surfaces only once a configured source
			// resolved it. Cheap Mongo $in lookup; response stays sub-second.
			matched, _ := h.EnrichedScenes.GetEnrichedScenesByMatchedSourcesAndIDs(ctx, "tpdb", ids, sources, perPage)
			matchedIDs := make(map[string]bool, len(matched))
			for _, s := range matched {
				matchedIDs[s.ID] = true
			}
			metas := enrichedScenesToMetas(orderScenesByItems(items, matched))
			if store != nil && len(metas) > 0 {
				_ = store.setTPDBMetas(ctx, cacheKey, metas)
			}
			// Fire-and-forget enrich for cold items (not yet matched) so the next
			// search surfaces them. Bounded conc; non-blocking - if busy, the
			// background sweep still populates the store next tick.
			h.enqueueEnrichedScenesOnDemand(cfg, items, matchedIDs)
			return CatalogResponse{Metas: metas}, nil
		}
	}
	return h.serveTPDBSearchLocal(ctx, cfg, contentType, searchQ, skip)
}

// enrichedOnDemandSem bounds concurrent on-demand store-population goroutines
// fired by tpdb_search live hits, so a burst of searches does not fan out
// unbounded scraper matching. Non-blocking acquire: if busy, skip - the background
// EnrichedScenesSync sweep will still populate the store next tick.
// ponytail: a single global cap is coarse; per-source/per-user locks if a hot
// search term saturates the 4 slots and starves other searches.
var enrichedOnDemandSem = make(chan struct{}, 4)

// enqueueEnrichedScenesOnDemand fires a bounded, fire-and-forget goroutine that
// upserts the cold (not-yet-matched) live TPDB search items into enriched_scenes and
// torrent-matches them against the user's configured sources, so the next search
// surfaces them once a source resolves. No-op when no callback is wired, no sources
// are configured, every item is already matched, or the semaphore is busy. The search
// response is already rendered; this only affects subsequent searches.
func (h *Handler) enqueueEnrichedScenesOnDemand(cfg Config, items []map[string]interface{}, matchedIDs map[string]bool) {
	if h.EnrichedScenesOnDemand == nil || len(items) == 0 {
		return
	}
	sources := matchedSourcesFilter(cfg)
	if len(sources) == 0 {
		return
	}
	var cold []map[string]interface{}
	for _, item := range items {
		id := tpdbSceneID(item)
		if id == "" || matchedIDs[id] {
			continue
		}
		cold = append(cold, item)
	}
	if len(cold) == 0 {
		return
	}
	select {
	case enrichedOnDemandSem <- struct{}{}:
	default:
		return // busy; the background sweep handles it
	}
	go func() {
		defer func() { <-enrichedOnDemandSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h.EnrichedScenesOnDemand(ctx, cold, sources)
	}()
}

// orderScenesByItems returns matched in the order their TPDB item appeared in the
// live search response, preserving TPDB relevance ranking instead of the store's
// date:-1 sort. Scenes without a matching item id are dropped.
func orderScenesByItems(items []map[string]interface{}, matched []models.EnrichedScene) []models.EnrichedScene {
	byID := make(map[string]models.EnrichedScene, len(matched))
	for _, s := range matched {
		byID[s.ID] = s
	}
	out := make([]models.EnrichedScene, 0, len(matched))
	for _, item := range items {
		if id := tpdbSceneID(item); id != "" {
			if s, ok := byID[id]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// sourcesKey is a stable, debuggable cache-key suffix for a configured source
// set: lowercase, sorted, '|' joined. Two users with different cfg.Sources must
// not share a filtered tpdb_search page, so the key is part of the cache key.
// ponytail: a sorted join over the small source set is collision-safe without a
// hash lib and stays readable in `redis-cli KEYS`.
func sourcesKey(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	cp := make([]string, len(sources))
	for i, s := range sources {
		cp[i] = strings.ToLower(s)
	}
	sort.Strings(cp)
	return strings.Join(cp, "|")
}

// tpdbMetasFromItems builds + caches the TPDB-scene metas for live items. Used
// only by the cold-install degradation path in serveTPDBSearch (no EnrichedScenes
// store wired): returns every TPDB hit unfiltered. The store-backed path renders
// metas from enriched_scenes via enrichedScenesToMetas instead.
func (h *Handler) tpdbMetasFromItems(ctx context.Context, store *redisStore, cacheKey string, items []map[string]interface{}, err error) (CatalogResponse, error) {
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
			Poster:      bg,
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

// serveTPDBSearchLocal renders a tpdb_search query from the enriched
// pornrips_entries store, reusing the pr_search pipeline (fetchPornripsFromStore
// dispatches pr_search -> SearchPornrips over title/resolved_title/performers ->
// entriesToCatalog -> buildMetas) so a performer/title query surfaces PRT releases
// when the TPDB API has no scene. Only runs when cfg.Sources contains pornrips -
// the gate the old path lacked (it fell back to pornrips regardless of cfg.Sources).
// Uncached: a cheap Mongo scan, kept off the TPDB cache so a later TPDB hit for the
// same query is not served a stale local result.
func (h *Handler) serveTPDBSearchLocal(ctx context.Context, cfg Config, contentType, searchQ string, skip int) (CatalogResponse, error) {
	if !sourceEnabled(cfg, "pornrips") {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	torrents := h.fetchPornripsFromStore(ctx, cfg, "pr_search", "", searchQ, skip)
	metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "", false)
	if err != nil || metas == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	return CatalogResponse{Metas: metas}, nil
}

// serveTPDBMeta fetches single-scene metadata. Store-first: read
// enriched_scenes by the porndb:<num> id and build the Meta from the stored fields,
// so a detail open does not hit the live TPDB API. Falls back to a live GetScene
// on a store miss (cold-start safety - the bulk fill may not have reached this
// scene yet); the fallback is removed in Phase 5 once the fill is confirmed
// complete. id is the catalog item ID, e.g. "porndb:11093443".
func (h *Handler) serveTPDBMeta(ctx context.Context, id string) (*Meta, error) {
	if h.EnrichedScenes != nil {
		if scene, err := h.EnrichedScenes.GetEnrichedSceneByID(ctx, id); err == nil && scene != nil && scene.Title != "" {
			return enrichedSceneToMeta(scene), nil
		}
	}
	// The live fallback is TPDB-only (it calls tpdb.GetScene on the numeric id).
	// stash: ids have no live StashDB meta fallback wired, so a stash: store miss
	// returns nil rather than burning TPDB throttle budget on a malformed
	// "stash:<sceneId>" GetScene lookup (extractPornDBNumericID only strips the
	// porndb: prefix and would pass the stash: id through unchanged).
	if !strings.HasPrefix(id, "porndb:") {
		return nil, nil
	}
	return h.serveTPDBMetaLive(ctx, id)
}

// serveTPDBMetaLive is the cold-start fallback: a live TPDB GetScene rendered to a
// Meta. Kept until the bulk fill is confirmed complete (Phase 5 removes it).
func (h *Handler) serveTPDBMetaLive(ctx context.Context, id string) (*Meta, error) {
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

	return &Meta{
		ID:          id,
		Type:        "Porn",
		Name:        name,
		Poster:      bg,
		Background:  bg,
		Description: desc,
		ReleaseInfo: year,
		Genres:      genres,
		Links:       metaLinks(cast, genres),
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

	// Hentai self-scrape (Phase C): hmm- ids resolve direct mp4 streams from
	// the durable hentai_entries store + source scraper. Legacy hs:/hse-/hs-/
	// htv- ids are deprecated and fall through to the empty-stream return below.
	if strings.HasPrefix(id, "hmm-") {
		streams := h.serveHentaiStream(r.Context(), id)
		if streams == nil {
			streams = []map[string]interface{}{}
		}
		writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
		return
	}

	// Tube sources (pvz: / fpv: / and later ypv: / wpt: / pec: / phd: / p4d: /
	// hqp:) resolve direct streams from their durable *_entries store. The
	// generic serveTubeStream stamps Cloudflare-gate proxyHeaders. h.TubeSources
	// is nil only in bare-handler tests.
	if h.TubeSources != nil {
		if src := h.TubeSources.LookupByIDPrefix(id); src != nil {
			streams := h.serveTubeStream(r.Context(), src, id)
			if streams == nil {
				streams = []map[string]interface{}{}
			}
			writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
			return
		}
	}

	// jstrm:/jstrg: catalog item IDs (PornRips, TPB, Sukebei). Enriched
	// PornRips items carry h:<infoHash> and are emitted directly so direct-backend
	// Stremio installs get P2P streams without the Node edge; un-enriched items
	// return no streams until the background PornripsSync job backfills the hash.
	if strings.HasPrefix(id, "jstrm:") || strings.HasPrefix(id, "jstrg:") {
		streams := h.jstrmOrGroupStreams(r.Context(), id)
		if streams == nil {
			streams = []map[string]interface{}{}
		}
		writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
		return
	}

	if !strings.HasPrefix(id, "porndb:") && !strings.HasPrefix(id, "stash:") {
		writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": []interface{}{}})
		return
	}

	cfg := ParseConfig(r.PathValue("config"))
	streams := h.tpdbStreams(r.Context(), cfg, id)
	if streams == nil {
		streams = []map[string]interface{}{}
	}
	writeStremioJSON(w, http.StatusOK, map[string]interface{}{"streams": streams})
}

// jstrmStreams resolves a jstrm: catalog item ID into infoHash streams for
// direct-backend Stremio installs (which bypass the Node edge's debrid/P2P
// resolver). Mongo-only: items that already carry an infoHash (the enriched
// payload's h:<infoHash>) are emitted directly; items without one return no
// streams until the background PornripsSync job backfills the hash. No live
// pornrips.to detail-page fetch on the request path.
func (h *Handler) jstrmStreams(ctx context.Context, id string) []map[string]interface{} {
	payload := DecodeItemID(id)
	if payload == nil {
		return nil
	}
	infoHash := strings.ToLower(strings.TrimSpace(payload.H))
	if infoHash == "" {
		return nil
	}
	title := payload.T
	if title == "" {
		title = "Unknown"
	}
	name := "Torrent"
	if payload.W == "pornrips" {
		name = "PRT"
	}

	return []map[string]interface{}{{
		"infoHash":      infoHash,
		"name":          name,
		"title":         title,
		"behaviorHints": map[string]interface{}{"notWebReady": true},
	}}
}

// jstrmOrGroupStreams resolves a jstrm: item ID or a jstrg: group ID into
// infoHash streams for direct-backend Stremio installs (which bypass the Node
// edge's debrid/P2P resolver). A jstrg: group encodes several variants of one
// scene; each member is re-encoded as a jstrm: ID and resolved per-record,
// mirroring the Node edge's stream.ts group path.
func (h *Handler) jstrmOrGroupStreams(ctx context.Context, id string) []map[string]interface{} {
	if strings.HasPrefix(id, "jstrg:") {
		group := DecodeGroupID(id)
		if len(group) == 0 {
			return nil
		}
		var out []map[string]interface{}
		for _, p := range group {
			member := EncodeItemID(TorrentRecord{
				InfoHash: p.H, Title: p.T, TorrentURL: p.U,
				Website: p.W, DetailURL: p.D, Quality: p.Q,
			})
			if s := h.jstrmStreams(ctx, member); len(s) > 0 {
				out = append(out, s...)
			}
		}
		return out
	}
	return h.jstrmStreams(ctx, id)
}

// tpdbStreams resolves a porndb:<id> item into infoHash streams from the
// enriched_scenes store. The store holds the per-source best torrent for each
// scene; emit one stream per matched source the user has configured
// (matchedSourcesFilter), piratebay preferred over pornrips (higher quality
// scrape). Redis-fronted by numID (getPornStreams/setPornStreams) so a re-open
// is served from cache. No live TPDB/pornrips lookup on the request path. Cold
// store (EnrichedScenes nil) or a scene with no matched source -> no streams.
func (h *Handler) tpdbStreams(ctx context.Context, cfg Config, id string) []map[string]interface{} {
	numID := extractPornDBNumericID(id)
	if numID == "" {
		return nil
	}
	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getPornStreams(ctx, numID); ok {
			return cached
		}
	}
	if h.EnrichedScenes == nil {
		return nil
	}
	scene, err := h.EnrichedScenes.GetEnrichedSceneByID(ctx, id)
	if err != nil || scene == nil {
		return nil
	}
	out := sceneStreams(scene, matchedSourcesFilter(cfg))
	if store != nil && len(out) > 0 {
		_ = store.setPornStreams(ctx, numID, out)
	}
	return out
}

// tpdbClient creates a TPDB client from the Handler environment, or returns nil.
func (h *Handler) tpdbClient() *metadata.TPDBClient {
	if h.Env == nil || h.Env.Metadata.TPDBAPIKey == "" {
		return nil
	}
	return metadata.NewTPDBClient(h.Env.Metadata.TPDBAPIURL, h.Env.Metadata.TPDBAPIKey)
}

// pornripsSlugFromDetail extracts the WP slug from a pornrips.to/<slug>/ detail URL,
// matching the pornrips_entries _id key "pr:" + slug.
func pornripsSlugFromDetail(detailURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(detailURL), "/")
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return trimmed
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

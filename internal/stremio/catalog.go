package stremio

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/internal/services/scraper"
)

const (
	tpdbCatalogID    = "tpdb_cat"
	stashdbCatalogID = "stashdb_cat"
	catalogScraper   = "piratebay"

	refCatalogConcurrency = 6
	refCatalogTimeout     = 4500 * time.Millisecond
)

type hbParams struct {
	Website  string
	Category string
	Query    string
	Sort     string
	Mode     string
}

// ServeCatalog returns Stremio catalog metas for the given catalog id.
func (h *Handler) ServeCatalog(ctx context.Context, cfg Config, contentType, catalogID, extra string) (CatalogResponse, error) {
	params := parseExtra(extra)
	skip := atoiDefault(params["skip"], 0)
	searchQ := params["search"]
	genre := params["genre"]

	if catalogID == tpdbCatalogID || catalogID == stashdbCatalogID {
		return h.serveCategoryCatalog(ctx, catalogID, genre, skip, cfg.MaxResults, cfg)
	}

	if !isCatalogAllowed(cfg, catalogID) {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	// Compact studio catalog: merge the studio's enabled quality x sort variants
	// into one response. Bare xxx_studio_{slug} ids only appear when compact mode
	// is on, so they never reach the single-variant scrape path below.
	if cfg.CompactStudios && isCompactStudioCatalogID(catalogID) {
		return h.serveCompactStudioCatalog(ctx, cfg, strings.TrimPrefix(catalogID, "xxx_studio_"), contentType, skip, cfg.MaxResults)
	}

	// Compact main XXX catalog: bare `xxx` merges the 4K + 1080p variants into
	// one scene-grouped response. Only appears when compact mode is on.
	if cfg.CompactStudios && catalogID == "xxx" {
		return h.serveCompactMainXxxCatalog(ctx, cfg, contentType, skip, cfg.MaxResults)
	}

	// Compact Trans catalog: bare `xxx_trans` merges its 4K + 1080p variants the
	// same way. Only appears when compact mode is on.
	if cfg.CompactStudios && catalogID == "xxx_trans" {
		return h.serveCompactTransCatalog(ctx, cfg, contentType, skip, cfg.MaxResults)
	}

	if strings.HasPrefix(catalogID, "tpdb_") {
		return h.serveTPDBCatalog(ctx, catalogID, searchQ, skip)
	}

	if strings.HasPrefix(catalogID, "hentai_") {
		return h.serveProxiedCatalog(ctx, catalogID, genre, searchQ, skip)
	}

	if strings.HasPrefix(catalogID, "pr_") {
		return h.servePornripsCatalog(ctx, cfg, catalogID, contentType, genre, searchQ, skip)
	}

	if strings.HasPrefix(catalogID, "sukebei_") {
		return h.serveSukebeiCatalog(ctx, cfg, catalogID, searchQ, skip, cfg.MaxResults)
	}

	if strings.HasPrefix(catalogID, "sc_") {
		return h.serveStripchatCatalog(ctx, catalogID, searchQ, skip, cfg.MaxResults)
	}

	if !isPornSearchCatalogID(catalogID) && !strings.HasPrefix(catalogID, "xxx_") {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	torrents, err := h.loadCatalogTorrents(ctx, cfg, catalogID, contentType, searchQ, genre, skip)
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}

	// Quality scope for xxx_ catalogs: "_fhd" suffix = 1080p catalog, else 4K.
	quality := catalogQualityScope(catalogID)

	metas, err := h.buildMetas(ctx, cfg, torrents, contentType, quality)
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	return CatalogResponse{Metas: metas}, nil
}

func isCatalogAllowed(cfg Config, catalogID string) bool {
	// Search is a utility catalog, not browse - always available so a whitelist
	// (EnabledCatalogs) user isn't advertised a row that returns empty.
	if isPornSearchCatalogID(catalogID) {
		return true
	}
	// Compact studio catalog: bare xxx_studio_{slug} (no sort suffix). Allowed
	// when any quality base for the studio is enabled (or not disabled). Sorts
	// are merged at serve time, so the EnabledSorts suffix check below is skipped.
	if cfg.CompactStudios && isCompactStudioCatalogID(catalogID) {
		return compactStudioSelected(cfg, catalogID)
	}
	// Compact main XXX catalog: bare `xxx` is allowed when either 4K (xxx) or
	// 1080p (xxx_fhd) base is enabled (or not disabled), since it merges both.
	if cfg.CompactStudios && catalogID == "xxx" {
		return baseEnabled(cfg, "xxx") || baseEnabled(cfg, "xxx_fhd")
	}
	// Compact Trans catalog: bare `xxx_trans` is allowed when either 4K
	// (xxx_trans) or 1080p (xxx_trans_fhd) base is enabled (or not disabled).
	if cfg.CompactStudios && catalogID == "xxx_trans" {
		return baseEnabled(cfg, "xxx_trans") || baseEnabled(cfg, "xxx_trans_fhd")
	}
	isOwn := strings.HasPrefix(catalogID, "pr_") ||
		strings.HasPrefix(catalogID, "tpdb_") ||
		strings.HasPrefix(catalogID, "hentai_") ||
		strings.HasPrefix(catalogID, "sukebei_") ||
		strings.HasPrefix(catalogID, "sc_")
	base := catalogID
	if !isOwn {
		base = strings.TrimSuffix(strings.TrimSuffix(catalogID, "_top"), "_recent")
	}
	if isOwn {
		for _, d := range cfg.DisabledCatalogs {
			if d == base || (strings.HasPrefix(catalogID, "sukebei_") && d == "sukebei") {
				return false
			}
		}
		return true
	}
	if len(cfg.EnabledCatalogs) > 0 {
		for _, e := range cfg.EnabledCatalogs {
			if e == base {
				return true
			}
		}
		return false
	}
	for _, d := range cfg.DisabledCatalogs {
		if d == base {
			return false
		}
	}
	// Defense in depth: respect the sort-variant allow-list for TPB-style ids.
	if sort := catalogSortSuffix(catalogID); sort != "" {
		for _, s := range cfg.EnabledSorts {
			if s == sort {
				return true
			}
		}
		return false
	}
	return true
}

// isCompactStudioCatalogID reports whether catalogID is a bare compact studio
// id: `xxx_studio_{slug}` with no sort suffix. Real variant ids always end in
// `_top` or `_recent`, so absence of that suffix distinguishes the compact form.
func isCompactStudioCatalogID(catalogID string) bool {
	if !strings.HasPrefix(catalogID, "xxx_studio_") {
		return false
	}
	return !strings.HasSuffix(catalogID, "_top") && !strings.HasSuffix(catalogID, "_recent")
}

// baseEnabled reports whether a quality base (e.g. xxx_studio_vixen or its
// _fhd variant) is on for this install: present in the EnabledCatalogs
// allow-list, or - with no allow-list - absent from DisabledCatalogs.
func baseEnabled(cfg Config, base string) bool {
	if len(cfg.EnabledCatalogs) > 0 {
		for _, e := range cfg.EnabledCatalogs {
			if e == base {
				return true
			}
		}
		return false
	}
	for _, d := range cfg.DisabledCatalogs {
		if d == base {
			return false
		}
	}
	return true
}

// compactStudioSelected reports whether a bare compact studio id should be
// served: any of the studio's quality bases is enabled (or not disabled).
// 1080p-only studios only have the _fhd base, so only that base is consulted.
func compactStudioSelected(cfg Config, bareID string) bool {
	studio := resolveStudioQuery(strings.TrimPrefix(bareID, "xxx_studio_"))
	_, only1080 := studio1080pOnly[studio]
	if only1080 {
		return baseEnabled(cfg, bareID+"_fhd")
	}
	return baseEnabled(cfg, bareID) || baseEnabled(cfg, bareID+"_fhd")
}

// maxCompactFetchPages caps how many underlying catalog pages compact mode will
// scrape per variant when scrolling. ponytail: 20 pages × maxResults step; raise
// if users routinely need deeper infinite scroll.
const maxCompactFetchPages = 20

type compactSceneMember struct {
	t catalogTorrent
}

type compactSceneGroup struct {
	members []compactSceneMember // sorted by seeders desc; members[0] is the representative
}

// countCompactSceneGroups returns distinct scene groups in a stamped torrent list.
func countCompactSceneGroups(torrents []catalogTorrent) int {
	seen := make(map[string]struct{}, len(torrents))
	for _, t := range torrents {
		seen[compactSceneKey(t.Title)] = struct{}{}
	}
	return len(seen)
}

// buildCompactSceneGroups groups stamped torrents by scene title and ranks groups
// by the highest seeder count in each group.
func buildCompactSceneGroups(merged []catalogTorrent) []compactSceneGroup {
	groupOrder := make([]string, 0, len(merged))
	groups := make(map[string][]compactSceneMember)
	for _, t := range merged {
		key := compactSceneKey(t.Title)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], compactSceneMember{t: t})
	}
	gs := make([]compactSceneGroup, 0, len(groupOrder))
	for _, k := range groupOrder {
		ms := groups[k]
		sort.Slice(ms, func(i, j int) bool { return ms[i].t.Seeders > ms[j].t.Seeders })
		gs = append(gs, compactSceneGroup{members: ms})
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].members[0].t.Seeders > gs[j].members[0].t.Seeders })
	return gs
}

// loadCompactMergedTorrents fetches enough underlying quality×sort pages to cover
// needGroups scene groups after dedupe and quality stamping. Non-compact catalogs
// advance skip per variant; compact mode must fan out across variants instead.
func (h *Handler) loadCompactMergedTorrents(ctx context.Context, cfg Config, base4k, baseFhd string, only1080 bool, contentType string, sorts []string, needGroups int) []catalogTorrent {
	include4k := !only1080 && baseEnabled(cfg, base4k)
	includeFhd := baseEnabled(cfg, baseFhd)
	if (!include4k && !includeFhd) || len(sorts) == 0 || needGroups <= 0 {
		return nil
	}

	step := cfg.MaxResults
	if step <= 0 {
		step = 20
	}

	merged := make([]catalogTorrent, 0)
	for page := 0; page < maxCompactFetchPages; page++ {
		underlyingSkip := page * step
		batch := make([]catalogTorrent, 0)
		collect := func(base, q string) {
			for _, s := range sorts {
				toks, err := h.loadCatalogTorrents(ctx, cfg, base+"_"+s, contentType, "", "", underlyingSkip)
				if err != nil {
					continue
				}
				for i := range toks {
					toks[i].Quality = q
					batch = append(batch, toks[i])
				}
			}
		}
		if include4k {
			collect(base4k, "4k")
		}
		if includeFhd {
			collect(baseFhd, "fhd")
		}
		if len(batch) == 0 {
			break
		}
		merged = dedupeCatalogTorrents(append(merged, batch...))
		merged = filterByStampedQuality(merged)
		if countCompactSceneGroups(merged) >= needGroups {
			break
		}
	}
	return merged
}

// serveCompactStudioCatalog merges a studio's enabled quality x sort variant
// catalogs into one response. Each variant is read via loadCatalogTorrents
// (hitting the per-variant warmed cache key, with live scrape fallback), then
// results are deduped and sorted by seeders descending. 1080p results are
// included only when the studio's 1080p base is enabled; 4K only otherwise.
func (h *Handler) serveCompactStudioCatalog(ctx context.Context, cfg Config, slug, contentType string, skip, maxResults int) (CatalogResponse, error) {
	studio := resolveStudioQuery(slug)
	_, only1080 := studio1080pOnly[studio]
	return h.serveCompactMergedCatalog(ctx, cfg, "xxx_studio_"+slug, "xxx_studio_"+slug+"_fhd", only1080, contentType, skip, maxResults)
}

// serveCompactMainXxxCatalog compacts the main XXX catalog the same way studios
// are compacted: one bare `xxx` catalog merging the enabled 4K (xxx) and 1080p
// (xxx_fhd) variant caches, grouped by scene into jstrg: entries. The main xxx
// is never 1080p-only.
func (h *Handler) serveCompactMainXxxCatalog(ctx context.Context, cfg Config, contentType string, skip, maxResults int) (CatalogResponse, error) {
	return h.serveCompactMergedCatalog(ctx, cfg, "xxx", "xxx_fhd", false, contentType, skip, maxResults)
}

// serveCompactTransCatalog compacts the Trans catalog the same way as the main
// XXX: one bare `xxx_trans` catalog merging its 4K (xxx_trans) and 1080p
// (xxx_trans_fhd) variant caches, grouped by scene.
func (h *Handler) serveCompactTransCatalog(ctx context.Context, cfg Config, contentType string, skip, maxResults int) (CatalogResponse, error) {
	return h.serveCompactMergedCatalog(ctx, cfg, "xxx_trans", "xxx_trans_fhd", false, contentType, skip, maxResults)
}

// serveCompactMergedCatalog is the shared compact serve path for a quality-paired
// catalog (a studio or the main xxx). It reads the 4K (base4k) and 1080p
// (baseFhd) variant caches via loadCatalogTorrents, stamps per-variant quality,
// dedupes, enriches via buildMetas, groups by normalized scene title, and emits
// one jstrg: meta per scene encoding every variant - so the stream route
// returns one stream per quality. only1080 marks 1080p-only studios (no 4K base).
func (h *Handler) serveCompactMergedCatalog(ctx context.Context, cfg Config, base4k, baseFhd string, only1080 bool, contentType string, skip, maxResults int) (CatalogResponse, error) {
	include4k := !only1080 && baseEnabled(cfg, base4k)
	includeFhd := baseEnabled(cfg, baseFhd)
	if !include4k && !includeFhd {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	sorts := cfg.EnabledSorts
	if len(sorts) == 0 {
		// No sorts selected: mirror the non-compact path, which emits no
		// variants when EnabledSorts is empty.
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	if maxResults <= 0 {
		maxResults = 20
	}
	needGroups := skip + maxResults
	merged := h.loadCompactMergedTorrents(ctx, cfg, base4k, baseFhd, only1080, contentType, sorts, needGroups)
	if len(merged) == 0 {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	gs := buildCompactSceneGroups(merged)
	if skip > len(gs) {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	end := skip + maxResults
	if end > len(gs) {
		end = len(gs)
	}
	pageGroups := gs[skip:end]

	pageTorrents := make([]catalogTorrent, 0)
	for _, g := range pageGroups {
		for _, m := range g.members {
			pageTorrents = append(pageTorrents, m.t)
		}
	}
	metas, err := h.buildMetas(ctx, cfg, pageTorrents, contentType, "")
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}

	out := make([]MetaPreview, 0, len(pageGroups))
	metaIdx := 0
	for _, g := range pageGroups {
		recs := make([]TorrentRecord, 0, len(g.members))
		var mp MetaPreview
		for _, m := range g.members {
			r := catalogTorrentToRecord(m.t)
			r.Quality = m.t.Quality
			recs = append(recs, r)
			if mp.ID == "" && metaIdx < len(metas) {
				mp = metas[metaIdx]
			}
			metaIdx++
		}
		if mp.ID == "" && len(g.members) > 0 {
			mp = MetaPreview{Name: g.members[0].t.Title}
		}
		mp.ID = EncodeGroupID(recs)
		mp.Type = contentType
		mp.PosterShape = "landscape"
		out = append(out, mp)
	}
	return CatalogResponse{Metas: out}, nil
}

// compactSceneKey normalizes a torrent title for grouping the 4K and 1080p
// releases of one scene into a single compact catalog entry. It strips
// quality/codec tokens (reusing qualStripRE) and lowercases, leaving only the
// scene identity. Compact catalogs are per-studio, so collisions across
// different scenes are unlikely.
func compactSceneKey(title string) string {
	cleaned := strings.TrimSpace(strings.NewReplacer(".", " ", "_", " ").Replace(title))
	stripped := strings.TrimSpace(qualStripRE.ReplaceAllString(cleaned, ""))
	if stripped == "" {
		stripped = cleaned
	}
	return strings.ToLower(strings.TrimSpace(stripped))
}

func (h *Handler) serveCategoryCatalog(ctx context.Context, catalogID, genre string, skip, maxResults int, cfg Config) (CatalogResponse, error) {
	source := "tpdb"
	configured := cfg.TpdbCategories
	if catalogID == stashdbCatalogID {
		source = "stashdb"
		configured = cfg.StashdbCategories
	}

	store := newRedisStore(h.Redis)

	var entries []jobs.StremioMetaPreview
	if genre == "All" {
		// "All" aggregates every configured category for the source (deduped by ID).
		entries = mergeCategoryMetas(ctx, store, source, configured)
	} else {
		slug := nameToSlug(genre)
		if slug == "" {
			return CatalogResponse{Metas: []MetaPreview{}}, nil
		}
		var err error
		entries, err = store.getCategoryMetas(ctx, source, slug)
		if err != nil {
			return CatalogResponse{Metas: []MetaPreview{}}, err
		}
	}

	if maxResults <= 0 {
		maxResults = 20
	}
	end := skip + maxResults
	if skip > len(entries) {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[skip:end]
	enriched, err := h.enrichCategoryMetas(ctx, source, page)
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	metas := make([]MetaPreview, 0, len(enriched))
	for _, m := range enriched {
		metas = append(metas, MetaPreview{
			ID:          m.ID,
			Type:        m.Type,
			Name:        m.Name,
			Poster:      m.Poster,
			Background:  m.Background,
			Description: m.Description,
			ReleaseInfo: m.ReleaseInfo,
			// TPB covers are landscape scene stills; render them as wide cards.
			PosterShape: "landscape",
		})
	}
	return CatalogResponse{Metas: metas}, nil
}

// mergeCategoryMetas reads every configured category for a source and returns
// their entries concatenated in slug order, deduped by ID. Powers the "All"
// genre view, which aggregates all enabled categories.
func mergeCategoryMetas(ctx context.Context, store *redisStore, source string, slugs []string) []jobs.StremioMetaPreview {
	combined := make([]jobs.StremioMetaPreview, 0)
	seen := make(map[string]struct{})
	for _, slug := range slugs {
		entries, err := store.getCategoryMetas(ctx, source, slug)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.ID == "" {
				continue
			}
			if _, ok := seen[e.ID]; ok {
				continue
			}
			seen[e.ID] = struct{}{}
			combined = append(combined, e)
		}
	}
	return combined
}

func (h *Handler) enrichCategoryMetas(ctx context.Context, source string, metas []jobs.StremioMetaPreview) ([]jobs.StremioMetaPreview, error) {
	store := newRedisStore(h.Redis)
	out := make([]jobs.StremioMetaPreview, len(metas))
	copy(out, metas)
	for i := range out {
		if out[i].Poster != "" {
			continue
		}
		rec := DecodeItemID(out[i].ID)
		if rec == nil || rec.H == "" {
			continue
		}
		shared, err := store.getSharedMeta(ctx, source, rec.H)
		if err != nil || shared == nil || shared.Poster == "" {
			continue
		}
		out[i].Poster = shared.Poster
		if shared.Background != "" {
			out[i].Background = shared.Background
		} else {
			out[i].Background = shared.Poster
		}
	}
	return out, nil
}

func (h *Handler) loadCatalogTorrents(ctx context.Context, cfg Config, catalogID, contentType, searchQ, genre string, skip int) ([]catalogTorrent, error) {
	store := newRedisStore(h.Redis)
	cacheKey := buildCatalogListKey(h.catalogBaseURL(cfg), catalogID, contentType, searchQ, genre, skip, catalogFanoutKey(cfg))
	if store != nil {
		if cached, err := store.getCatalogTorrents(ctx, cacheKey); err == nil && len(cached) > 0 {
			return applyCatalogTorrentFilters(cached, catalogID, cfg), nil
		}
	}
	if h.Scrapers == nil {
		return nil, nil
	}
	fetched, err := h.fetchAdultCatalog(ctx, cfg, catalogID, searchQ, skip)
	if err != nil {
		return nil, err
	}
	// Cache the scraped list so paginated / searched / genre-filtered loads (which
	// the warmer never pre-fills) and cold-cache reads don't re-scrape on every
	// request - matching the Node addon's catalog route. Only non-empty lists are
	// cached: an empty result is usually a transient scraper hiccup, and caching
	// [] would propagate the outage to every later request.
	if store != nil && len(fetched) > 0 {
		_ = store.setTorrentList(ctx, prefixCatalogList+cacheKey, fetched, ttlCatalogList)
	}
	return applyCatalogTorrentFilters(fetched, catalogID, cfg), nil
}

// catalogBaseURL returns the first segment of the cat:v1: cache keys. It MUST
// equal the warmer's value (jobs.ResolveCatalogBaseURL) or warmed catalogs are
// never read. cfg.BackendURL is only a last-resort fallback for when neither
// ADDON_CACHE_BASE_URL nor BASE_URL is configured on this service.
func (h *Handler) catalogBaseURL(cfg Config) string {
	if base := jobs.ResolveCatalogBaseURL(h.Env); base != "" {
		return base
	}
	return cfg.BackendURL
}

func (h *Handler) fetchAdultCatalog(ctx context.Context, cfg Config, catalogID, searchQ string, skip int) ([]catalogTorrent, error) {
	params := getHbParams(catalogID)
	if params == nil {
		return nil, nil
	}
	page := skip/30 + 1
	opts := models.SearchOptions{Sort: params.Sort, Category: params.Category}

	var raw []models.Torrent
	var err error
	switch {
	case searchQ != "":
		terms := h.resolveSearchTerms(ctx, cfg, searchQ)
		raw, err = h.searchPirateBayTerms(ctx, terms, page, models.SearchOptions{Sort: "7"})
		raw = append(raw, h.fanoutAdultSources(ctx, cfg, searchQ, page, "7")...)
	case params.Query != "":
		raw, err = h.Scrapers.Search(ctx, catalogScraper, params.Query, page, opts)
		raw = append(raw, h.fanoutAdultSources(ctx, cfg, params.Query, page, params.Sort)...)
	default:
		raw, err = h.Scrapers.Browse(ctx, catalogScraper, params.Category, page, params.Sort, opts)
		raw = append(raw, h.fanoutAdultBrowse(ctx, cfg, params.Category, page)...)
	}
	if err != nil {
		if err == scraper.ErrScraperNotFound {
			return nil, nil
		}
		return nil, err
	}

	normalized := make([]catalogTorrent, 0, len(raw))
	for _, t := range raw {
		normalized = append(normalized, normalizeModelTorrent(t))
	}
	torrents := dedupeCatalogTorrents(normalized)
	torrents = filterByQualityScope(torrents, catalogQualityScope(catalogID))
	if params.Sort == "7" || searchQ != "" {
		sort.Slice(torrents, func(i, j int) bool { return torrents[i].Seeders > torrents[j].Seeders })
		// TPB results first, then other indexers (each group sorted by seeders).
		tpb := make([]catalogTorrent, 0, len(torrents))
		others := make([]catalogTorrent, 0, len(torrents))
		for _, t := range torrents {
			if t.Website == catalogScraper {
				tpb = append(tpb, t)
			} else {
				others = append(others, t)
			}
		}
		torrents = append(tpb, others...)
	}
	max := cfg.MaxResults
	if max <= 0 {
		max = 20
	}
	if len(torrents) > max {
		torrents = torrents[:max]
	}
	return torrents, nil
}

func (h *Handler) fanoutAdultSources(ctx context.Context, cfg Config, query string, page int, sort string) []models.Torrent {
	if h.Scrapers == nil {
		return nil
	}
	var sources []string
	if cfg.ExtraIndexers {
		sources = append(sources, "knaben_adult", "bitsearch")
	}
	if cfg.Enable1337x {
		sources = append(sources, "1337x")
	}
	if len(sources) == 0 {
		return nil
	}
	type result struct {
		torrents []models.Torrent
	}
	ch := make(chan result, len(sources))
	var wg sync.WaitGroup
	childCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for _, name := range sources {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := query
			if strings.EqualFold(strings.TrimSpace(query), "trans") {
				q = fanoutAdultSearchQuery(query)
			}
			t, err := h.Scrapers.Search(childCtx, name, q, page, models.SearchOptions{Sort: sort})
			if err != nil {
				ch <- result{}
				return
			}
			ch <- result{torrents: t}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	var out []models.Torrent
	for r := range ch {
		out = append(out, r.torrents...)
	}
	return filterByMinSeeders(out, cfg.MinSeeders)
}

// catalogFanoutKey tags Redis catalog-list cache entries with the indexer fanout
// flags that shaped the scrape. Without this, a fetch with extraIndexers enabled
// poisons the shared cache for installs that left Knaben/Bitsearch off.
func catalogFanoutKey(cfg Config) string {
	parts := []string{"hb"}
	if cfg.ExtraIndexers {
		parts = append(parts, "kn", "bs", "xc")
	}
	if cfg.Enable1337x {
		parts = append(parts, "1337")
	}
	return strings.Join(parts, ",")
}

func applyCatalogTorrentFilters(torrents []catalogTorrent, catalogID string, cfg Config) []catalogTorrent {
	t := filterByIndexerConfig(filterByQualityScope(torrents, catalogQualityScope(catalogID)), cfg)
	if isTransCatalogID(catalogID) {
		t = filterTransRelevance(t)
	}
	return t
}

// filterByIndexerConfig drops torrents from optional indexers when the install
// config has them disabled. Also scrubs stale cache rows written before fanout
// flags were part of the cache key.
func filterByIndexerConfig(torrents []catalogTorrent, cfg Config) []catalogTorrent {
	out := torrents[:0]
	for _, t := range torrents {
		src := t.Website
		if src == "" {
			src = t.Indexer
		}
		switch src {
		case "knaben", "bitsearch", "xxxclub":
			if !cfg.ExtraIndexers {
				continue
			}
		case "1337x":
			if !cfg.Enable1337x {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func (h *Handler) fanoutAdultBrowse(ctx context.Context, cfg Config, category string, page int) []models.Torrent {
	if h.Scrapers == nil || !cfg.ExtraIndexers {
		return nil
	}
	sources := []string{"xxxclub"}
	type result struct {
		torrents []models.Torrent
	}
	ch := make(chan result, len(sources))
	var wg sync.WaitGroup
	childCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for _, name := range sources {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			t, err := h.Scrapers.Browse(childCtx, name, category, page, "3", models.SearchOptions{})
			if err != nil {
				ch <- result{}
				return
			}
			ch <- result{torrents: t}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	var out []models.Torrent
	for r := range ch {
		out = append(out, r.torrents...)
	}
	return filterByMinSeeders(out, cfg.MinSeeders)
}

// filterByMinSeeders drops torrents below the seeder floor. It filters in
// place over the caller's backing array (out := ts[:0]); safe only because
// every caller discards the input immediately after. ponytail: in-place filter,
// allocate a fresh slice if a caller ever retains the input.
func filterByMinSeeders(ts []models.Torrent, min int) []models.Torrent {
	if min <= 0 {
		return ts
	}
	out := ts[:0]
	for _, t := range ts {
		if t.Seeders >= min {
			out = append(out, t)
		}
	}
	return out
}

// catalogQualityScope returns the resolution scope encoded into xxx_ catalog
// items: "fhd" for "_fhd" (1080p) catalogs, "4k" otherwise, "" for non-xxx.
func catalogQualityScope(catalogID string) string {
	if !strings.HasPrefix(catalogID, "xxx_") {
		return ""
	}
	if strings.Contains(catalogID, "_fhd") {
		return "fhd"
	}
	return "4k"
}

// filterByQualityScope drops catalog entries whose detected resolution doesn't
// match the catalog's scope. Unknown-quality titles (no resolution token) are
// kept, mirroring the stream-time filter, so a 4K catalog never advertises a
// 1080p-only torrent that would then resolve to zero playable streams.
func filterByQualityScope(torrents []catalogTorrent, scope string) []catalogTorrent {
	if scope == "" {
		return torrents
	}
	out := torrents[:0]
	for _, t := range torrents {
		q := detectQuality(t.Title)
		if scope == "4k" && (q == "1080p" || q == "720p" || q == "480p") {
			continue
		}
		if scope == "fhd" && (q == "2160p" || q == "720p" || q == "480p") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// filterByStampedQuality mirrors filterByQualityScope for compact merged catalogs,
// where each torrent carries an explicit Quality stamp ("4k" / "fhd") instead of
// deriving scope from the catalog id. Drops entries that would resolve to zero
// streams after the Node edge's per-member quality filter.
func filterByStampedQuality(torrents []catalogTorrent) []catalogTorrent {
	out := torrents[:0]
	for _, t := range torrents {
		q := detectQuality(t.Title)
		switch t.Quality {
		case "4k":
			if q == "1080p" || q == "720p" || q == "480p" {
				continue
			}
		case "fhd":
			if q == "2160p" || q == "720p" || q == "480p" {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func (h *Handler) buildMetas(ctx context.Context, cfg Config, torrents []catalogTorrent, contentType, quality string) ([]MetaPreview, error) {
	if len(torrents) == 0 {
		return []MetaPreview{}, nil
	}
	store := newRedisStore(h.Redis)
	metas := make([]MetaPreview, 0, len(torrents))
	enqueue := make([]jobs.MetaEnqueueItem, 0, len(torrents))
	noMerged := make([]bool, 0, len(torrents))

	for _, t := range torrents {
		rec := catalogTorrentToRecord(t)
		// Honor an explicit quality label; otherwise preserve a quality stamped
		// on the catalogTorrent by the caller (compact mode stamps per-variant
		// quality so group-id members carry their own resolution scope).
		if quality != "" {
			rec.Quality = quality
		} else {
			rec.Quality = t.Quality
		}
		id := EncodeItemID(rec)
		rec.ID = id
		if store != nil {
			_ = store.setTorrent(ctx, id, rec)
		}

		metaID := StableMetaID(rec.Website, rec.DetailURL, rec.InfoHash)
		var tpdb, stashdb *jobs.SharedMeta
		if store != nil && metaID != "" {
			tpdb, _ = store.getSharedMeta(ctx, "tpdb", metaID)
			stashdb, _ = store.getSharedMeta(ctx, "stashdb", metaID)
		}
		merged := mergeMetadata(tpdb, stashdb)
		noMerged = append(noMerged, merged == nil)

		title, year := ParseTorrentTitle(rec.Title)
		name := title
		if merged != nil && merged.Title != "" {
			name = merged.Title
		}
		poster := rec.CoverImage
		if merged != nil && merged.Poster != "" {
			poster = merged.Poster
		}
		if poster == "" {
			poster = h.resolveCover(ctx, rec)
		}
		desc := buildTorrentDescription(rec)
		if merged != nil && merged.Description != "" {
			desc = merged.Description
		}
		release := year
		if merged != nil && merged.Year != "" {
			release = merged.Year
		}

		metas = append(metas, MetaPreview{
			ID:          id,
			Type:        contentType,
			Name:        name,
			Poster:      poster,
			Background:  pickBackground(merged, poster),
			Description: desc,
			ReleaseInfo: release,
			// TPB covers are landscape scene stills; render them as wide cards.
			PosterShape: "landscape",
		})
		enqueue = append(enqueue, jobs.MetaEnqueueItem{
			Title:     rec.Title,
			DetailURL: rec.DetailURL,
			Website:   rec.Website,
			InfoHash:  rec.InfoHash,
		})
	}

	h.enrichCatalogLiveMeta(ctx, cfg, torrents, metas, noMerged)

	refs := h.resolveCatalogReferenceMeta(ctx, torrents, noMerged)
	for i := range metas {
		ref := refs[i]
		if ref == nil {
			continue
		}
		if ref.Name != "" {
			metas[i].Name = ref.Name
		} else if torrents[i].Website == "pornrips" {
			metas[i].Name = strings.TrimSpace(strings.ReplaceAll(torrents[i].Title, ".", " "))
		}
		if metas[i].Poster == "" && ref.Poster != "" {
			metas[i].Poster = ref.Poster
		}
		if metas[i].Background == "" {
			metas[i].Background = pickBackground(referenceToMerged(ref), metas[i].Poster)
		}
		if ref.Description != "" {
			metas[i].Description = ref.Description
		}
		if metas[i].ReleaseInfo == "" && ref.Year != "" {
			metas[i].ReleaseInfo = ref.Year
		}
	}

	h.enrichPornripsDetailCovers(ctx, torrents, metas)

	propagateMetaByJAVCode(torrents, metas)

	propagateMetaByOnlyFansPerformer(torrents, metas)

	if h.MetaEnqueuer != nil {
		h.MetaEnqueuer(ctx, enqueue)
	}
	return metas, nil
}

func (h *Handler) resolveCatalogReferenceMeta(ctx context.Context, torrents []catalogTorrent, noMerged []bool) []*metadata.ReferenceMeta {
	out := make([]*metadata.ReferenceMeta, len(torrents))
	need := make([]int, 0)
	for i, t := range torrents {
		if !noMerged[i] || t.Website != "pornrips" {
			continue
		}
		if slug := PornripsSlug(t.DetailURL); slug != "" {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, refCatalogTimeout)
	defer cancel()

	type result struct {
		idx  int
		meta *metadata.ReferenceMeta
	}
	ch := make(chan result, len(need))
	sem := make(chan struct{}, refCatalogConcurrency)
	var wg sync.WaitGroup
	for _, idx := range need {
		idx := idx
		slug := PornripsSlug(torrents[idx].DetailURL)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			meta := h.referenceMetaForSlug(ctx, slug, true)
			ch <- result{idx: idx, meta: meta}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	for r := range ch {
		out[r.idx] = r.meta
	}
	return out
}

func (h *Handler) enrichCatalogLiveMeta(ctx context.Context, cfg Config, torrents []catalogTorrent, metas []MetaPreview, noMerged []bool) {
	if strings.TrimSpace(cfg.TpdbKey) == "" && strings.TrimSpace(cfg.StashdbKey) == "" {
		if h.Env == nil || (h.Env.Metadata.TPDBAPIKey == "" && h.Env.Metadata.StashDBAPIKey == "") {
			return
		}
	}
	store := newRedisStore(h.Redis)
	need := make([]int, 0)
	for i, t := range torrents {
		if !noMerged[i] || strings.TrimSpace(metas[i].Poster) != "" {
			continue
		}
		// PornRips detail pages can supply covers separately; still allow live meta.
		if t.Title == "" {
			continue
		}
		need = append(need, i)
		if len(need) >= catalogLiveMetaMaxPerPage {
			break
		}
	}
	if len(need) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, catalogLiveMetaTimeout)
	defer cancel()

	type result struct {
		idx    int
		merged *jobs.SharedMeta
	}
	ch := make(chan result, len(need))
	sem := make(chan struct{}, catalogLiveMetaConcurrency)
	var wg sync.WaitGroup
	for _, idx := range need {
		idx := idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			t := torrents[idx]
			metaID := StableMetaID(t.Website, t.DetailURL, t.InfoHash)
			merged := h.loadMergedMeta(ctx, cfg, store, metaID, t.Title, t.DetailURL)
			if mergedHasPoster(merged) {
				ch <- result{idx: idx, merged: merged}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	for r := range ch {
		applyMergedToPreview(&metas[r.idx], r.merged)
		if metas[r.idx].Poster != "" {
			noMerged[r.idx] = false
		}
	}
}

func applyMergedToPreview(preview *MetaPreview, merged *jobs.SharedMeta) {
	if merged == nil {
		return
	}
	if merged.Title != "" {
		preview.Name = merged.Title
	}
	if merged.Poster != "" {
		preview.Poster = merged.Poster
	}
	if merged.Background != "" {
		preview.Background = merged.Background
	} else if preview.Poster != "" {
		preview.Background = preview.Poster
	}
	if merged.Description != "" {
		preview.Description = merged.Description
	}
	if merged.Year != "" {
		preview.ReleaseInfo = merged.Year
	}
}

// propagateMetaByJAVCode copies poster/title metadata from one catalog row to
// siblings that share the same JAV product code (e.g. ACHJ-030-FHD ← ACHJ-030).
func propagateMetaByJAVCode(torrents []catalogTorrent, metas []MetaPreview) {
	if len(torrents) != len(metas) || len(torrents) == 0 {
		return
	}
	best := make(map[string]int)
	for i, t := range torrents {
		key := javCodeKey(t.Title)
		if key == "" {
			continue
		}
		if cur, ok := best[key]; !ok || javMetaDonorBetter(metas, cur, i) {
			best[key] = i
		}
	}
	for i, t := range torrents {
		key := javCodeKey(t.Title)
		if key == "" {
			continue
		}
		donor := best[key]
		if donor == i {
			continue
		}
		applyDonorMetaPreview(&metas[i], &metas[donor])
	}
}

func javCodeKey(title string) string {
	code := metadata.ParseRelease(title).Code
	if code == "" {
		return ""
	}
	return metadata.NormalizedJAVCode(code)
}

func javMetaDonorBetter(metas []MetaPreview, donor, candidate int) bool {
	dp := strings.TrimSpace(metas[donor].Poster) != ""
	cp := strings.TrimSpace(metas[candidate].Poster) != ""
	if cp != dp {
		return cp
	}
	return len(metas[candidate].Name) > len(metas[donor].Name)
}

func applyDonorMetaPreview(dst, src *MetaPreview) {
	if strings.TrimSpace(src.Poster) == "" || strings.TrimSpace(dst.Poster) != "" {
		return
	}
	dst.Poster = src.Poster
	if strings.TrimSpace(src.Background) != "" {
		dst.Background = src.Background
	} else {
		dst.Background = src.Poster
	}
	if strings.TrimSpace(src.Name) != "" {
		dst.Name = src.Name
	}
	if strings.TrimSpace(src.Description) != "" && strings.TrimSpace(dst.Description) == "" {
		dst.Description = src.Description
	}
	if strings.TrimSpace(src.ReleaseInfo) != "" && strings.TrimSpace(dst.ReleaseInfo) == "" {
		dst.ReleaseInfo = src.ReleaseInfo
	}
}

func applyDonorPosterOnly(dst, src *MetaPreview) {
	if strings.TrimSpace(src.Poster) == "" || strings.TrimSpace(dst.Poster) != "" {
		return
	}
	dst.Poster = src.Poster
	if strings.TrimSpace(src.Background) != "" {
		dst.Background = src.Background
	} else {
		dst.Background = src.Poster
	}
}

// propagateMetaByOnlyFansPerformer copies poster art to OnlyFans dash-format rows
// that share the same primary performer on one catalog page (e.g. Anna Ralphs).
func propagateMetaByOnlyFansPerformer(torrents []catalogTorrent, metas []MetaPreview) {
	if len(torrents) != len(metas) || len(torrents) == 0 {
		return
	}
	performers := make(map[string]struct{})
	for _, t := range torrents {
		if key := onlyFansPerformerKey(t.Title); key != "" {
			performers[key] = struct{}{}
		}
	}
	if len(performers) == 0 {
		return
	}
	best := make(map[string]int)
	for i, t := range torrents {
		if strings.TrimSpace(metas[i].Poster) == "" {
			continue
		}
		lower := strings.ToLower(t.Title)
		for key := range performers {
			if strings.Contains(lower, key) {
				if cur, ok := best[key]; !ok || strings.TrimSpace(metas[cur].Poster) == "" {
					best[key] = i
				}
			}
		}
	}
	for i, t := range torrents {
		key := onlyFansPerformerKey(t.Title)
		if key == "" || strings.TrimSpace(metas[i].Poster) != "" {
			continue
		}
		donor, ok := best[key]
		if !ok || donor == i {
			continue
		}
		applyDonorPosterOnly(&metas[i], &metas[donor])
	}
}

func onlyFansPerformerKey(title string) string {
	p := metadata.ParseRelease(title)
	if !strings.EqualFold(p.Studio, "OnlyFans") || strings.TrimSpace(p.Performer) == "" {
		return ""
	}
	return strings.ToLower(metadata.PrimaryPerformer(p.Performer))
}

func pickBackground(merged *jobs.SharedMeta, poster string) string {
	if merged != nil && merged.Background != "" {
		return merged.Background
	}
	return poster
}

func catalogTorrentToRecord(t catalogTorrent) TorrentRecord {
	website := t.Website
	if website == "" {
		website = "piratebay"
	}
	indexer := t.Indexer
	if indexer == "" {
		indexer = website
	}
	return TorrentRecord{
		Title:      t.Title,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   t.InfoHash,
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		DetailURL:  t.DetailURL,
		Website:    website,
		Indexer:    indexer,
		CoverImage: t.CoverImage,
	}
}

func normalizeModelTorrent(t models.Torrent) catalogTorrent {
	cover := ""
	if t.CoverImage != nil {
		cover = t.CoverImage.URL
	}
	infoHash := ExtractInfoHash(t.MagnetLink)
	website := t.Website
	if website == "" {
		website = catalogScraper
	}
	detailURL := strings.TrimSpace(t.UploadedBy)
	if detailURL == "" || !strings.Contains(strings.ToLower(detailURL), "pornrips") {
		detailURL = t.TorrentURL
	}
	return catalogTorrent{
		Title:      t.Name,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   infoHash,
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		DetailURL:  detailURL,
		CoverImage: cover,
		Website:    website,
		Indexer:    website,
	}
}

func dedupeCatalogTorrents(torrents []catalogTorrent) []catalogTorrent {
	best := make(map[string]catalogTorrent)
	order := make([]string, 0, len(torrents))
	for _, t := range torrents {
		key := t.InfoHash
		if key == "" {
			key = t.Title
		}
		if key == "" {
			continue
		}
		if existing, ok := best[key]; ok {
			if t.Seeders > existing.Seeders {
				best[key] = t
			}
			continue
		}
		best[key] = t
		order = append(order, key)
	}
	out := make([]catalogTorrent, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
	}
	return out
}

func getHbParams(catalogID string) *hbParams {
	sortCode := "3"
	baseID := catalogID
	for _, v := range []struct{ suffix, sort string }{
		{"top", "7"},
		{"recent", "3"},
	} {
		suffix := "_" + v.suffix
		if strings.HasSuffix(baseID, suffix) {
			sortCode = v.sort
			baseID = strings.TrimSuffix(baseID, suffix)
			break
		}
	}

	category := "507"
	if strings.HasSuffix(baseID, "_fhd") {
		category = "505"
		baseID = strings.TrimSuffix(baseID, "_fhd")
	}

	var query string
	switch {
	case baseID == "xxx", isPornSearchCatalogID(baseID):
		// Search is search-only; browse mode just falls back to a generic
		// listing (Stremio only requests it with a search term).
		query = ""
	case baseID == "xxx_trans":
		query = "trans"
	case strings.HasPrefix(baseID, "xxx_studio_"):
		slug := strings.TrimPrefix(baseID, "xxx_studio_")
		query = resolveStudioQuery(slug)
	default:
		return nil
	}

	mode := "browse"
	if query != "" {
		mode = "search"
	}
	return &hbParams{
		Website:  catalogScraper,
		Category: category,
		Query:    query,
		Sort:     sortCode,
		Mode:     mode,
	}
}

// resolveStudioQuery maps a studio catalog slug back to the exact preset name
// (e.g. "sean_cody" -> "Sean Cody", "men_com" -> "Men.com") so a cache-miss
// live scrape searches the same term the manifest/warmer use. Falls back to the
// slug with underscores replaced by spaces for unknown (extra-KV) studios.
func resolveStudioQuery(slug string) string {
	for _, studio := range StudioPresets {
		if studioSafeID(studio) == slug {
			return studio
		}
	}
	return strings.ReplaceAll(slug, "_", " ")
}

func nameToSlug(displayName string) string {
	for _, c := range jobs.AllCategories {
		if c.Name == displayName {
			return c.Slug
		}
	}
	return ""
}

func parseExtra(extra string) map[string]string {
	out := map[string]string{}
	if extra == "" {
		return out
	}
	decoded, err := url.QueryUnescape(extra)
	if err != nil {
		decoded = extra
	}
	vals, err := url.ParseQuery(decoded)
	if err != nil {
		return out
	}
	for k, v := range vals {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func buildTorrentDescription(t TorrentRecord) string {
	parts := make([]string, 0, 4)
	if qt := QualityTag(t.Title); qt != "" && qt != "Unknown" {
		parts = append(parts, qt)
	}
	if t.Size != "" {
		parts = append(parts, t.Size)
	}
	if t.Seeders > 0 {
		parts = append(parts, strconv.Itoa(t.Seeders)+" seeders")
	}
	if t.Indexer != "" {
		parts = append(parts, "via "+t.Indexer)
	}
	return strings.Join(parts, " | ")
}

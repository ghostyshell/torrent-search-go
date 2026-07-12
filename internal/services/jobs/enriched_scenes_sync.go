package jobs

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"time"

	"torrent-search-go/internal/cache"
	im "torrent-search-go/internal/models"
	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/pkg/models"
)

const (
	enrichedDefaultPerTick   = 50
	enrichedDefaultConc      = 4
	enrichedDefaultStashCats  = 18 // covers the Default:true categories; bulk-fill raises it
	enrichedDiscoverPerPage  = 100
	enrichedStashPerPage      = 25
	// enrichedDiscoverRetries rides out a TPDB 429/5xx storm per page so a
	// transient rate-limit doesn't abandon the stride (which would leave pages
	// unwalked and keep discoveryOK=false, forcing a full re-walk every tick).
	// The loop does 1 initial call + this many retries (= 7 attempts total);
	// doGet already honored one Retry-After per attempt, this just keeps the
	// stride alive. Worst case ~5min per stuck page, well inside the tick ctx.
	enrichedDiscoverRetries = 6
)

// EnrichedScenesSync populates the enriched_scenes store. Each tick it (A)
// discovers TPDB scenes via BrowseScenes and StashDB scenes via FetchScenes per
// category, upserting scene stubs with metadata but no torrent sources, then (B)
// torrent-matches the configured source-set against scenes not yet attempted
// for each source. The tpdb_new / tpdb_cat / stashdb_cat catalogs and the
// porndb: meta/stream path then read the store instead of hitting the live
// TPDB/StashDB APIs, and the matched_sources $in query gates scenes to the
// user's configured torrent sources - the source-config gate the prior live path
// lacked (it cross-referenced pornrips regardless of cfg.Sources).
//
// Source-set comes from ENRICHED_SCENES_SOURCES (comma-separated, default
// "piratebay"); the local bulk-fill sets the full porn set
// (piratebay,knaben_adult,1337x,xxxclub,pornrips), the deployed job keeps
// the default. "pornrips" is a cross-link to pornrips_entries by performers, not a
// scrape. Discovery re-walking an already-stored scene is a no-op (metadata is
// $setOnInsert, sources $addToSet-empty), so it is safe to run every tick.
func (r *Runner) EnrichedScenesSync(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo not configured"}, nil
	}
	// Default to the full porn source-set so the deployed job matches each new TPDB
	// scene across every configured source (not piratebay-only). The bulk-fill and
	// deployed job both override via env; this default covers the no-env deployed
	// path so new scenes surface in every source's catalog without a container tweak.
	sources := splitCSV(envOr("ENRICHED_SCENES_SOURCES", "piratebay,knaben_adult,1337x,xxxclub,pornrips"))
	disc := r.discoverEnrichedScenes(ctx)
	match := r.matchEnrichedScenes(ctx, sources)
	out := map[string]interface{}{"success": true}
	for k, v := range disc {
		out[k] = v
	}
	for k, v := range match {
		out[k] = v
	}
	return out, nil
}

// EnrichScenesOnDemand is the on-demand store populator fired by a successful
// tpdb_search live hit: it upserts each live TPDB scene as a stub ($setOnInsert
// metadata, so a re-search is a no-op) then torrent-matches it against the user's
// configured sources (the matchedSourcesFilter the catalog read path uses), so
// the scene surfaces in the store-backed tpdb_new browse on the next open instead
// of requiring the background sweep. Reuses matchSceneSource, so the on-demand
// match agrees with the sweep on what resolves. Fire-and-forget: the search
// response is returned before this runs; bounded by the caller's timeout and the
// per-call conc cap. A transient scrape failure leaves the source out of
// attempted_sources so the background sweep retries it.
func (r *Runner) EnrichScenesOnDemand(ctx context.Context, items []map[string]interface{}, sources []string) {
	if r.storage == nil || len(items) == 0 || len(sources) == 0 {
		return
	}
	conc := envInt("ENRICHED_SCENES_ONDEMAND_CONCURRENCY", 4)
	if conc < 1 {
		conc = 4
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			break
		}
		rawID := metadata.SceneIDFromItem(item)
		if rawID == "" {
			continue
		}
		s := metadata.NormalizeTPDBScene(item)
		scene := models.EnrichedScene{
			ID:          "porndb:" + rawID,
			Source:      "tpdb",
			Title:       s.Title,
			Poster:      s.Poster,
			Background:  s.Poster,
			Description: s.Description,
			Cast:        s.Performers,
			Tags:        s.Tags,
			TagsNorm:    normTags(s.Tags),
			Date:        s.Date,
			Studio:      s.Studio,
		}
		// Stub upsert first ($setOnInsert metadata), so meta/stream reads hit the
		// store even if matching finds nothing this round.
		_ = r.storage.UpsertEnrichedScene(ctx, scene)
		for _, src := range sources {
			wg.Add(1)
			sem <- struct{}{}
			go func(source string) {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				ref, attempted := r.matchSceneSource(ctx, scene, source)
				if !attempted {
					return
				}
				upd := models.EnrichedScene{
					ID:              scene.ID,
					Source:          scene.Source,
					AttemptedSources: []string{source},
				}
				if ref != nil {
					upd.MatchedSources = []string{source}
					upd.Torrents = map[string]models.TorrentRef{source: *ref}
				}
				_ = r.storage.UpsertEnrichedScene(ctx, upd)
			}(src)
		}
	}
	wg.Wait()
}

// discoverEnrichedScenes walks the newest TPDB BrowseScenes pages and a bounded set
// of StashDB category tags, upserting scene stubs. TPDB discovery is
// category-agnostic (BrowseScenes returns newest scenes with their tags, so the
// tpdb_cat tag filter works off scene.Tags); StashDB has no "browse all" endpoint,
// so it walks category tags. Returns discoveredTPDB / discoveredStash counts.
func (r *Runner) discoverEnrichedScenes(ctx context.Context) map[string]interface{} {
	out := map[string]interface{}{"discoveredTPDB": 0, "discoveredStash": 0, "discoveryOK": true}
	// Default to 5 pages of recent TPDB BrowseScenes so the deployed job picks up
	// new scenes added since the last tick with a safety margin (page 1 usually
	// suffices at the 10min cadence, but 5 covers a burst). The bulk-fill overrides
	// this to 200 (and to date-windowed mode via ENRICHED_SCENES_DISCOVER_FROM_YEAR).
	pages := envInt("ENRICHED_SCENES_DISCOVER_PAGES", 5)
	tpdbKeys := splitCSV(r.cfg.Metadata.TPDBAPIKey)
	stashKeys := splitCSV(r.cfg.Metadata.StashDBAPIKey)
	if len(tpdbKeys) > 0 {
		n, err := r.discoverTPDBScenes(ctx, tpdbKeys, r.cfg.Metadata.TPDBAPIURL, pages)
		out["discoveredTPDB"] = n
		if err != nil {
			out["discoveryOK"] = false
		}
	}
	if len(stashKeys) > 0 {
		var redisStore *addonRedisStore
		if r.redis != nil {
			redisStore = newAddonRedisStore(r.redis)
		}
		n, err := r.discoverStashScenes(ctx, stashKeys, redisStore)
		out["discoveredStash"] = n
		if err != nil {
			out["discoveryOK"] = false
		}
	}
	return out
}

// discoverTPDBScenes walks TPDB BrowseScenes pages, striding them across the key
// pool so each key's rate limit is used in parallel: key i walks pages i+1,
// i+nKeys+1, ... Each stride breaks at its first empty/short page, so the walk
// stops at the real feed end regardless of how the keys divide the pages. With a
// single key (the deployed job's default) this is identical to the old sequential
// walk. Returns the total discovered count and a non-nil error if any stride was
// interrupted (so the caller can retry instead of treating a partial walk as done).
func (r *Runner) discoverTPDBScenes(ctx context.Context, tpdbKeys []string, url string, pages int) (int, error) {
	n := len(tpdbKeys)
	if n == 0 {
		return 0, nil
	}
	// Full-catalog mode: walk monthly date windows from ENRICHED_SCENES_DISCOVER_FROM_YEAR
	// to now. The /scenes endpoint hard-caps total at 10000 (Laravel paginator), so plain
	// page-based walking only ever reaches the newest ~10000; date=YYYY-MM-01 escapes the
	// cap per month. Unset = the deployed shallow page walk (unchanged). Gated on pages>0
	// so the bulk-fill's match-only switch (DISCOVER_PAGES=0 after tick 1) disables this
	// too - otherwise tick 2 would re-walk every month.
	if fromYear := envInt("ENRICHED_SCENES_DISCOVER_FROM_YEAR", 0); fromYear > 0 && pages > 0 {
		return r.discoverTPDBScenesDateWindows(ctx, tpdbKeys, url, fromYear)
	}
	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, n)
	var wg sync.WaitGroup
	for i, key := range tpdbKeys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			tpdb := metadata.NewTPDBClient(url, key)
			local := 0
			for page := i + 1; page <= pages; page += n {
				if err := ctx.Err(); err != nil {
					resCh <- result{local, err}
					return
				}
				// Retry the page on transient 429/5xx so a rate-limit hiccup doesn't
				// exit the stride and leave the rest of its pages unwalked. Only
				// abandon the page after enrichedDiscoverRetries consecutive fails.
				var items []map[string]interface{}
				var err error
				for try := 0; ; try++ {
					items, err = tpdb.BrowseScenes(ctx, page, enrichedDiscoverPerPage)
					if err == nil || try >= enrichedDiscoverRetries || ctx.Err() != nil {
						break
					}
					select {
					case <-time.After(time.Duration(2*(try+1)) * time.Second):
					case <-ctx.Done():
					}
				}
				if err != nil {
					resCh <- result{local, err}
					return
				}
				if len(items) == 0 {
					break
				}
				for _, item := range items {
					rawID := metadata.SceneIDFromItem(item)
					if rawID == "" {
						continue
					}
					s := metadata.NormalizeTPDBScene(item)
					_ = r.storage.UpsertEnrichedScene(ctx, models.EnrichedScene{
						ID:          "porndb:" + rawID,
						Source:      "tpdb",
						Title:       s.Title,
						Poster:      s.Poster,
						Background:  s.Poster,
						Description: s.Description,
						Cast:        s.Performers,
						Tags:        s.Tags,
						TagsNorm:    normTags(s.Tags),
						Date:        s.Date,
						Studio:      s.Studio,
					})
					local++
				}
				if len(items) < enrichedDiscoverPerPage {
					break
				}
			}
			resCh <- result{local, nil}
		}(i, key)
	}
	wg.Wait()
	close(resCh)
	total := 0
	var firstErr error
	for res := range resCh {
		total += res.n
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}
	return total, firstErr
}

// discoverTPDBScenesDateWindows walks the full TPDB catalog as monthly date windows
// (the only way past the /scenes 10000 hard-cap). Windows are generated newest-first
// from fromYear to the current month; each key strides the window list (key i walks
// windows[i], windows[i+n], ...) so all keys run in parallel. Each window is paged
// via BrowseScenesDate until a page returns empty or short, with the same per-page
// 429/5xx retry as the page-based walk. Upserts are idempotent, so a restart re-walks
// stored windows as no-ops (bounded to the real catalog, not the uncapped feed).
func (r *Runner) discoverTPDBScenesDateWindows(ctx context.Context, tpdbKeys []string, url string, fromYear int) (int, error) {
	n := len(tpdbKeys)
	windows := tpdbDateWindows(fromYear)
	if len(windows) == 0 {
		return 0, nil
	}
	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, n)
	var wg sync.WaitGroup
	for i, key := range tpdbKeys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			tpdb := metadata.NewTPDBClient(url, key)
			local := 0
			for w := i; w < len(windows); w += n {
				if err := ctx.Err(); err != nil {
					resCh <- result{local, err}
					return
				}
				for page := 1; ; page++ {
					var items []map[string]interface{}
					var err error
					for try := 0; ; try++ {
						items, err = tpdb.BrowseScenesDate(ctx, windows[w], page, enrichedDiscoverPerPage)
						if err == nil || try >= enrichedDiscoverRetries || ctx.Err() != nil {
							break
						}
						select {
						case <-time.After(time.Duration(2*(try+1)) * time.Second):
						case <-ctx.Done():
						}
					}
					if err != nil {
						resCh <- result{local, err}
						return
					}
					if len(items) == 0 {
						break
					}
					for _, item := range items {
						rawID := metadata.SceneIDFromItem(item)
						if rawID == "" {
							continue
						}
						s := metadata.NormalizeTPDBScene(item)
						_ = r.storage.UpsertEnrichedScene(ctx, models.EnrichedScene{
							ID:          "porndb:" + rawID,
							Source:      "tpdb",
							Title:       s.Title,
							Poster:      s.Poster,
							Background:  s.Poster,
							Description: s.Description,
							Cast:        s.Performers,
							Tags:        s.Tags,
							TagsNorm:    normTags(s.Tags),
							Date:        s.Date,
						})
						local++
					}
					if len(items) < enrichedDiscoverPerPage {
						break
					}
				}
			}
			resCh <- result{local, nil}
		}(i, key)
	}
	wg.Wait()
	close(resCh)
	total := 0
	var firstErr error
	for res := range resCh {
		total += res.n
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}
	return total, firstErr
}

// tpdbDateWindows returns monthly "YYYY-MM-01" date strings newest-first, from the
// current month down to fromYear-January inclusive. TPDB stores release dates
// month-anchored to the 1st and the /scenes endpoint hard-caps total at 10000, so
// each monthly window is a sub-10000 set with its own total - walking them reaches
// the full catalog past the newest 10000.
func tpdbDateWindows(fromYear int) []string {
	if fromYear < 2000 {
		fromYear = 2000
	}
	now := time.Now()
	y, m := now.Year(), int(now.Month())
	var out []string
	for {
		out = append(out, fmt.Sprintf("%04d-%02d-01", y, m))
		if y == fromYear && m == 1 {
			break
		}
		m--
		if m < 1 {
			m = 12
			y--
		}
		if y < fromYear {
			break
		}
	}
	return out
}

// discoverStashScenes walks up to stashCats category tags per tick (default the
// Default:true set; bulk-fill raises it to cover AllCategories), striding them
// across the StashDB key pool so each key's rate limit runs in parallel: key i
// walks categories i, i+n, i+2n, ... Each goroutine builds its own client (per-
// key throttle), retries FetchScenes on transient 429/5xx, and dedups scenes by
// stash id (a scene under multiple tags upserts once). With a single key this is
// a sequential walk over the categories. Returns the discovered count and a
// non-nil error if any goroutine was interrupted.
//
// Fixes three prod bugs in the prior version: (1) only stashKeys[0] was used, so
// extra keys never raised throughput; (2) the cap counted scenes not categories
// - default 18 stopped after ~18 scenes (<1 category) instead of 18 categories;
// (3) FetchScenes had no retry, so a single 429 dropped the rest of the walk.
func (r *Runner) discoverStashScenes(ctx context.Context, stashKeys []string, store *addonRedisStore) (int, error) {
	n := len(stashKeys)
	if n == 0 {
		return 0, nil
	}
	stashCats := envInt("ENRICHED_SCENES_STASH_CATS_PER_TICK", enrichedDefaultStashCats)
	cats := OrderedCategories()
	// Cap on CATEGORIES walked, not scenes. Default 18 = the Default:true set;
	// bulk-fill sets it above len(cats) to cover all.
	if stashCats > 0 && stashCats < len(cats) {
		cats = cats[:stashCats]
	}
	type result struct{ n int; err error }
	resCh := make(chan result, n)
	var wg sync.WaitGroup
	for i, key := range stashKeys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			stash := metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, key)
			cache := store.stashTagCache()
			local := 0
			for j := i; j < len(cats); j += n {
				if err := ctx.Err(); err != nil {
					resCh <- result{local, err}
					return
				}
				cat := cats[j]
				// Retry on transient 429/5xx so a rate-limit hiccup doesn't drop the
				// rest of this key's categories; mirror the TPDB page-retry backoff.
				var scenes []metadata.Scene
				var err error
				for try := 0; ; try++ {
					scenes, err = stash.FetchScenes(ctx, cache, cat.StashTag, enrichedStashPerPage)
					if err == nil || try >= enrichedDiscoverRetries || ctx.Err() != nil {
						break
					}
					select {
					case <-time.After(time.Duration(2*(try+1)) * time.Second):
					case <-ctx.Done():
					}
				}
				if err != nil {
					continue // transient: skip this category, retry next tick
				}
				for _, sc := range scenes {
					if sc.ID == "" {
						continue
					}
					_ = r.storage.UpsertEnrichedScene(ctx, models.EnrichedScene{
						ID:          "stash:" + sc.ID,
						Source:      "stashdb",
						Title:       sc.Title,
						Poster:      sc.Poster,
						Background:  sc.Poster,
						Description:  sc.Description,
						Cast:        sc.Performers,
						Tags:        sc.Tags,
						TagsNorm:    normTags(sc.Tags),
						Date:        sc.Date,
						Studio:      sc.Studio,
					})
					local++
				}
			}
			resCh <- result{local, nil}
		}(i, key)
	}
	wg.Wait()
	close(resCh)
	total := 0
	var firstErr error
	for res := range resCh {
		total += res.n
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}
	return total, firstErr
}

// matchEnrichedScenes fans the configured source-set across scenes not yet
// attempted for each source. Per source it pulls a perTick-bounded batch of
// scenes missing that source, matches them concurrently, and upserts the result
// (unioning matched_sources/attempted_sources + per-source torrent). A transient
// scrape failure returns attempted=false so the source is NOT added to
// attempted_sources and retries next tick; a clean hit or clean miss marks
// attempted so it is not re-scraped.
func (r *Runner) matchEnrichedScenes(ctx context.Context, sources []string) map[string]interface{} {
	out := map[string]interface{}{"matchScanned": 0, "matchAttempted": 0, "torrentResolved": 0}
	if r.scrapers == nil || len(sources) == 0 {
		return out
	}
	perTick := envInt("ENRICHED_SCENES_PER_TICK", enrichedDefaultPerTick)
	conc := envInt("ENRICHED_SCENES_CONCURRENCY", enrichedDefaultConc)

	var (
		mu              sync.Mutex
		scanned, tried, resolved int
	)
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			break
		}
		scenes, err := r.storage.GetEnrichedScenesMissingSourceMatch(ctx, src, perTick)
		if err != nil || len(scenes) == 0 {
			continue
		}
		mu.Lock()
		scanned += len(scenes)
		mu.Unlock()

		sem := make(chan struct{}, conc)
		var wg sync.WaitGroup
		for _, sc := range scenes {
			if err := ctx.Err(); err != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(scene models.EnrichedScene, source string) {
				defer wg.Done()
				defer func() { <-sem }()
				ref, attempted := r.matchSceneSource(ctx, scene, source)
				mu.Lock()
				tried++
				mu.Unlock()
				if !attempted {
					return // transient - leave attempted_sources untouched so it retries
				}
				upd := models.EnrichedScene{
					ID:              scene.ID,
					Source:          scene.Source,
					AttemptedSources: []string{source},
				}
				if ref != nil {
					upd.MatchedSources = []string{source}
					upd.Torrents = map[string]models.TorrentRef{source: *ref}
					mu.Lock()
					resolved++
					mu.Unlock()
				}
				_ = r.storage.UpsertEnrichedScene(ctx, upd)
			}(sc, src)
		}
		wg.Wait()
	}
	out["matchScanned"] = scanned
	out["matchAttempted"] = tried
	out["torrentResolved"] = resolved
	// Newly-resolved scenes are now eligible to surface in the tpdb_new browse
	// catalog, but that catalog page is Redis-cached (15min) keyed by source set.
	// Bust the tpdb_new keys so a freshly-resolved scene surfaces on the next
	// read instead of waiting out the TTL. Only tpdb_new (not the query-keyed
	// tpdb_search pages, which re-probe live TPDB on a miss and are cheaply
	// re-cached on demand), and only when at least one torrent resolved so a
	// clean-miss tick does not churn the cache. DelByPrefix is nil-safe.
	// ponytail: the "tpdb_new||" literal mirrors the browse-key prefix built in
	// serveTPDBBrowse (tpdb_catalog.go); if that key shape changes, update both.
	// Promote to a shared constant if a third site needs the same prefix.
	if resolved > 0 && r.redis != nil {
		if n, err := r.redis.DelByPrefix(ctx, cache.PrefixTPDBCatalog+"tpdb_new||"); err != nil {
			out["cacheBustErr"] = err.Error()
		} else {
			out["cacheBusted"] = n
		}
	}
	return out
}

// matchSceneSource resolves one scene against one torrent source. Returns the
// matched TorrentRef (nil on a clean miss) and attempted=true when the source
// produced a clean result (hit or miss) safe to record in attempted_sources;
// attempted=false signals a transient failure so the caller retries next tick.
func (r *Runner) matchSceneSource(ctx context.Context, scene models.EnrichedScene, source string) (*models.TorrentRef, bool) {
	switch source {
	case "piratebay":
		// findMatchingTorrent (category_warmer.go) paginates piratebay (all cats,
		// the scraper hardcodes cat 0) and tries performer then title query.
		// ENRICHED_SCENES_TPB_MAX_PAGES widens recall (default 1 = page 1 only,
		// the deployed CategoryWarmer depth); VerifyMatch keeps precision.
		ms := enrichedSceneToMetadata(scene)
		t, err := r.findMatchingTorrent(ctx, ms, envInt("ENRICHED_SCENES_TPB_MAX_PAGES", 1))
		if err != nil {
			return nil, false
		}
		if t == nil {
			return nil, true
		}
		ref := torrentToRef(t)
		return &ref, true
	case "pornrips":
		return r.matchPornripsScene(ctx, scene)
	default:
		// knaben_adult / 1337x / xxxclub: free-text search (the scraper
		// defaults to its porn category), sort by seeders, VerifyMatch the results.
		// ENRICHED_SCENES_MATCH_MAX_PAGES widens recall past page 1 (default 1);
		// VerifyMatch keeps precision.
		ms := enrichedSceneToMetadata(scene)
		query := sceneQuery(ms)
		if query == "" {
			return nil, true
		}
		candidate := metadata.MatchCandidate{
			Title:      ms.Title,
			Studio:     ms.Studio,
			Performers: ms.Performers,
			Date:       ms.Date,
		}
		maxPages := envInt("ENRICHED_SCENES_MATCH_MAX_PAGES", 1)
		for page := 1; page <= maxPages; page++ {
			torrents, err := r.scrapers.Search(ctx, source, query, page, im.SearchOptions{Sort: "7"})
			if err != nil {
				return nil, false
			}
			for _, t := range torrents {
				if metadata.VerifyMatch(metadata.ParseRelease(t.Name), candidate) {
					ref := torrentToRef(&t)
					return &ref, true
				}
			}
			if len(torrents) == 0 {
				break
			}
		}
		return nil, true
	}
}

// matchPornripsScene cross-links the scene to a pornrips_entries release by
// performers (the enrichment the PornripsSync job already wrote), so the pornrips
// torrent source emits a stream without re-enriching. Among performer-matched
// entries (already filtered to those with a resolved info_hash), prefer one whose
// resolved/WP title matches the scene title so the link is the same scene, not
// just any of the performer's releases; with no title overlap it is a clean miss.
// ponytail: performer+title substring match is approximate; tighten to an exact
// title+date match if the loose link surfaces wrong releases.
func (r *Runner) matchPornripsScene(ctx context.Context, scene models.EnrichedScene) (*models.TorrentRef, bool) {
	if len(scene.Cast) == 0 {
		return nil, true
	}
	entries, err := r.storage.GetPornripsEntriesByPerformers(ctx, scene.Cast, 3)
	if err != nil {
		return nil, false
	}
	if len(entries) == 0 {
		return nil, true
	}
	needle := strings.ToLower(strings.TrimSpace(scene.Title))
	var fallback *models.PornripsEntry
	for i := range entries {
		e := &entries[i]
		if e.InfoHash == "" {
			continue
		}
		if needle != "" && (strings.Contains(strings.ToLower(e.ResolvedTitle), needle) ||
			strings.Contains(strings.ToLower(e.Title), needle) ||
			strings.Contains(needle, strings.ToLower(e.ResolvedTitle))) {
			return pornripsEntryToRef(e), true
		}
		if fallback == nil {
			fallback = e
		}
	}
	if fallback != nil && needle == "" {
		return pornripsEntryToRef(fallback), true
	}
	return nil, true
}

// enrichedSceneToMetadata rebuilds a metadata.Scene for findMatchingTorrent /
// sceneQuery, which are keyed off Title/Studio/Performers/Date.
func enrichedSceneToMetadata(s models.EnrichedScene) metadata.Scene {
	return metadata.Scene{
		Title:      s.Title,
		Studio:     s.Studio,
		Performers: s.Cast,
		Tags:       s.Tags,
		Date:       s.Date,
	}
}

func torrentToRef(t *im.Torrent) models.TorrentRef {
	return models.TorrentRef{
		InfoHash:   extractInfoHash(t.MagnetLink),
		TorrentURL: t.TorrentURL,
		Title:      t.Name,
		Seeders:    t.Seeders,
	}
}

func pornripsEntryToRef(e *models.PornripsEntry) *models.TorrentRef {
	return &models.TorrentRef{
		InfoHash:   e.InfoHash,
		TorrentURL: e.TorrentURL,
		Title:      e.Title,
	}
}

func normTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if n := models.NormToken(t); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}
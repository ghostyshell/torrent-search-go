package jobs

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/images"
	"torrent-search-go/internal/services/metadata"
)

const (
	descMaxErrors      = 30
	descHomeQuery      = "xxx"
	descTransQuery     = "trans"
	descBrowseSort     = "3"
	descSearchSort     = "7"
	descTorrentSleep   = 250 * time.Millisecond
	descPageSleep      = 250 * time.Millisecond
	descProgressEvery  = 50
	descProgressSearch = 10

	// descDefaultMetaBudget bounds how many inline TPDB/StashDB lookups a single
	// run may perform (override with DESC_META_MAX_LOOKUPS). Reuse of already
	// resolved metadata from the shared store is unbounded and free.
	descDefaultMetaBudget = 400
)

var descCategories = []struct {
	category string
	label    string
}{
	{category: "507", label: "4K"},
	{category: "505", label: "1080p"},
}

// DescriptionImageCacheOptions configures a description/image cache run.
type DescriptionImageCacheOptions struct {
	ForceRefresh bool
	meta         *descMetaResolver
}

// descMetaResolver carries the TPDB/StashDB clients, the Redis shared-meta store
// and the per-run inline-lookup budget used to resolve metadata during a crawl.
type descMetaResolver struct {
	tpdb     *metadata.TPDBClient
	stash    *metadata.StashDBClient
	store    *addonRedisStore
	budget   int
	tpdbKey  string
	stashKey string
}

func descMetaLookupBudget() int {
	n := descDefaultMetaBudget
	if v, err := strconv.Atoi(os.Getenv("DESC_META_MAX_LOOKUPS")); err == nil && v >= 0 {
		n = v
	}
	return n
}

func (r *Runner) newDescMetaResolver() *descMetaResolver {
	res := &descMetaResolver{budget: descMetaLookupBudget()}
	if r.redis != nil {
		res.store = newAddonRedisStore(r.redis)
	}
	if r.cfg != nil {
		res.tpdbKey = r.cfg.Metadata.TPDBAPIKey
		res.stashKey = r.cfg.Metadata.StashDBAPIKey
		if res.tpdbKey != "" {
			res.tpdb = metadata.NewTPDBClient(r.cfg.Metadata.TPDBAPIURL, res.tpdbKey)
		}
		if res.stashKey != "" {
			res.stash = metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, res.stashKey)
		}
	}
	return res
}

// loadSharedMeta returns merged TPDB/StashDB metadata for a metaID, reading the
// durable Mongo store first and falling back to Redis for any source Mongo lacks.
func (r *Runner) loadSharedMeta(ctx context.Context, store *addonRedisStore, metaID string) *SharedMeta {
	if metaID == "" {
		return nil
	}
	var tpdb, stash *SharedMeta
	if tp, sp, err := r.storage.GetSharedMetaPair(ctx, metaID); err == nil {
		tpdb = payloadToShared(tp)
		stash = payloadToShared(sp)
	}
	if store != nil {
		if tpdb == nil {
			if m, err := store.GetSharedMeta(ctx, "tpdb", metaID); err == nil {
				tpdb = m
			}
		}
		if stash == nil {
			if m, err := store.GetSharedMeta(ctx, "stashdb", metaID); err == nil {
				stash = m
			}
		}
	}
	return MergeShared(tpdb, stash)
}

// resolveInline performs a bounded TPDB+StashDB lookup for an unresolved entry,
// persisting any hit to both the durable Mongo store and Redis (so the addon's
// MetaEnricher skips it) and returning the merged result.
func (res *descMetaResolver) resolveInline(ctx context.Context, r *Runner, metaID, title, detailURL string, results map[string]interface{}) *SharedMeta {
	if res == nil || metaID == "" || res.budget <= 0 || title == "" {
		return nil
	}
	if res.tpdb == nil && res.stash == nil {
		return nil
	}
	res.budget--
	results["metaLookups"] = results["metaLookups"].(int) + 1

	var tpdb, stash *SharedMeta
	if res.tpdb != nil {
		if nm, err := res.tpdb.SearchMetadata(ctx, title); err == nil && nm != nil {
			sm := normalizedToShared(nm)
			tpdb = &sm
			_ = r.storage.SetSharedMeta(ctx, "tpdb", metaID, sharedToPayload(tpdb))
			if res.store != nil {
				_ = res.store.SetSharedMeta(ctx, "tpdb", metaID, sm)
			}
		}
	}
	if res.stash != nil {
		if nm, err := res.stash.SearchMetadata(ctx, title, detailURL); err == nil && nm != nil {
			sm := normalizedToShared(nm)
			stash = &sm
			_ = r.storage.SetSharedMeta(ctx, "stashdb", metaID, sharedToPayload(stash))
			if res.store != nil {
				_ = res.store.SetSharedMeta(ctx, "stashdb", metaID, sm)
			}
		}
	}
	return MergeShared(tpdb, stash)
}

// coverSourceOf reports the cover source label for merged metadata.
func coverSourceOf(merged *SharedMeta) string {
	if merged == nil || merged.Source == "" {
		return "tpdb"
	}
	return merged.Source
}

// isUpgradedCoverSource reports whether a stored cover already came from a
// metadata provider (anything other than the NFO/description fallbacks).
func isUpgradedCoverSource(s string) bool {
	return s != "" && s != "nfo" && s != "description"
}

// pickFallbackCover returns the NFO/description cover URL and its source label.
func pickFallbackCover(details *models.TorrentDetails) (string, string) {
	if details.CoverImageURL != "" {
		return details.CoverImageURL, "nfo"
	}
	if len(details.Images) > 0 {
		candidates := details.Images
		if len(candidates) > 3 {
			candidates = candidates[:3]
		}
		selected := candidates[rand.Intn(len(candidates))]
		u := selected.DirectURL
		if u == "" {
			u = selected.OriginalURL
		}
		if u != "" {
			return u, "description"
		}
	}
	return "", ""
}

// enqueueDescMeta queues an entry for the MetaEnricher so a later run can upgrade
// its cover once TPDB/StashDB resolves a match.
func (r *Runner) enqueueDescMeta(title, detailURL, website, infoHash string) {
	if r.metaQueue == nil {
		return
	}
	r.EnqueueMetaLookups([]MetaEnqueueItem{{
		Title:     title,
		DetailURL: detailURL,
		Website:   website,
		InfoHash:  infoHash,
	}})
}

// DescriptionImageCache scrapes listings and stores cover images (Node parity).
func (r *Runner) DescriptionImageCache(ctx context.Context, forceRefresh bool) (map[string]interface{}, error) {
	if r.scrapers == nil {
		return map[string]interface{}{"success": false, "error": "scraper service not configured"}, fmt.Errorf("scraper service not configured")
	}

	pages := r.cfg.BackgroundJobs.DescriptionImageCachePages
	if pages.PagesBrowseHome <= 0 {
		pages.PagesBrowseHome = 3
	}
	if pages.PagesHomeQuery <= 0 {
		pages.PagesHomeQuery = 2
	}
	if pages.PagesTrans < 0 {
		pages.PagesTrans = 1
	}
	if pages.PagesPerStudio <= 0 {
		pages.PagesPerStudio = 2
	}

	opts := DescriptionImageCacheOptions{ForceRefresh: forceRefresh, meta: r.newDescMetaResolver()}

	results := map[string]interface{}{
		"totalSearches": 0,
		"totalTorrents": 0,
		"imagesFound":   0,
		"cached":        0,
		"replaced":      0,
		"skipped":       0,
		"failed":        0,
		"metaReused":    0,
		"metaResolved":  0,
		"metaLookups":   0,
		"coverUpgraded": 0,
		"descUpdated":   0,
		"errors":        []map[string]interface{}{},
	}
	errors := results["errors"].([]map[string]interface{})

	start := time.Now()
	log.Printf("[DescImageCache] Starting job (forceRefresh=%v, pages=%+v)", opts.ForceRefresh, pages)

	for i, cat := range descCategories {
		log.Printf("[DescImageCache] === %s (category %s) [%d/%d] ===", cat.label, cat.category, i+1, len(descCategories))

		for page := 1; page <= pages.PagesBrowseHome; page++ {
			if err := r.processDescBrowsePage(ctx, page, cat.category, opts, results, &errors); err != nil {
				return results, err
			}
		}

		for page := 1; page <= pages.PagesHomeQuery; page++ {
			if err := r.processDescSearchPage(ctx, descHomeQuery, page, cat.category, opts, results, &errors); err != nil {
				return results, err
			}
		}

		for page := 1; page <= pages.PagesTrans; page++ {
			if err := r.processDescSearchPage(ctx, descTransQuery, page, cat.category, opts, results, &errors); err != nil {
				return results, err
			}
		}

		studios := StudioSearchTerms()
		log.Printf("[DescImageCache] Studio pass: %d studios, %d pages each", len(studios), pages.PagesPerStudio)
		for si, studio := range studios {
			for page := 1; page <= pages.PagesPerStudio; page++ {
				if err := r.processDescSearchPage(ctx, studio, page, cat.category, opts, results, &errors); err != nil {
					return results, err
				}
			}
			if (si+1)%10 == 0 {
				log.Printf("[DescImageCache] Studio progress %d/%d, elapsed %v", si+1, len(studios), time.Since(start).Round(time.Second))
			}
		}
	}

	log.Printf("[DescImageCache] Job finished in %v - totalSearches=%d totalTorrents=%d imagesFound=%d cached=%d skipped=%d failed=%d metaReused=%d metaResolved=%d metaLookups=%d coverUpgraded=%d descUpdated=%d",
		time.Since(start).Round(time.Second),
		results["totalSearches"].(int),
		results["totalTorrents"].(int),
		results["imagesFound"].(int),
		results["cached"].(int),
		results["skipped"].(int),
		results["failed"].(int),
		results["metaReused"].(int),
		results["metaResolved"].(int),
		results["metaLookups"].(int),
		results["coverUpgraded"].(int),
		results["descUpdated"].(int))

	results["success"] = true
	results["errors"] = errors
	return results, nil
}

func (r *Runner) processDescBrowsePage(ctx context.Context, page int, category string, opts DescriptionImageCacheOptions, results map[string]interface{}, errors *[]map[string]interface{}) error {
	results["totalSearches"] = results["totalSearches"].(int) + 1
	torrents, err := r.scrapers.Browse(ctx, scraperWebsite, category, page, descBrowseSort, models.SearchOptions{})
	if err != nil {
		results["failed"] = results["failed"].(int) + 1
		pushJobError(errors, descMaxErrors, map[string]interface{}{"browse": true, "page": page, "error": err.Error()})
		log.Printf("[DescImageCache] Browse failed page %d cat %s: %v", page, category, err)
		return nil
	}
	if len(torrents) == 0 {
		log.Printf("[DescImageCache] No browse results page %d cat %s", page, category)
		return nil
	}
	log.Printf("[DescImageCache] Browse page %d cat %s: %d torrents", page, category, len(torrents))
	results["totalTorrents"] = results["totalTorrents"].(int) + len(torrents)
	for i, t := range torrents {
		if err := r.processDescTorrent(ctx, t, opts, results, errors); err != nil {
			return err
		}
		if (i+1)%descProgressEvery == 0 {
			log.Printf("[DescImageCache] Browse page %d cat %s progress %d/%d torrents", page, category, i+1, len(torrents))
		}
		if err := sleepCtx(ctx, descTorrentSleep); err != nil {
			return err
		}
	}
	return sleepCtx(ctx, descPageSleep)
}

func (r *Runner) processDescSearchPage(ctx context.Context, query string, page int, category string, opts DescriptionImageCacheOptions, results map[string]interface{}, errors *[]map[string]interface{}) error {
	results["totalSearches"] = results["totalSearches"].(int) + 1
	torrents, err := r.scrapers.Search(ctx, scraperWebsite, query, page, models.SearchOptions{
		Sort:     descSearchSort,
		Category: category,
	})
	if err != nil {
		results["failed"] = results["failed"].(int) + 1
		pushJobError(errors, descMaxErrors, map[string]interface{}{"query": query, "page": page, "error": err.Error()})
		log.Printf("[DescImageCache] Search failed %q page %d: %v", query, page, err)
		return nil
	}
	if len(torrents) == 0 {
		log.Printf("[DescImageCache] No results for %q page %d cat %s", query, page, category)
		return nil
	}
	log.Printf("[DescImageCache] Search %q page %d cat %s: %d torrents", query, page, category, len(torrents))
	results["totalTorrents"] = results["totalTorrents"].(int) + len(torrents)
	for i, t := range torrents {
		if err := r.processDescTorrent(ctx, t, opts, results, errors); err != nil {
			return err
		}
		if (i+1)%descProgressEvery == 0 {
			log.Printf("[DescImageCache] Search %q page %d cat %s progress %d/%d torrents", query, page, category, i+1, len(torrents))
		}
		if err := sleepCtx(ctx, descTorrentSleep); err != nil {
			return err
		}
	}

	searches := results["totalSearches"].(int)
	if searches%descProgressSearch == 0 {
		log.Printf("[DescImageCache] Search progress: %d searches, %d torrents, %d cached", searches, results["totalTorrents"].(int), results["cached"].(int))
	}
	return sleepCtx(ctx, descPageSleep)
}

func (r *Runner) processDescTorrent(ctx context.Context, t models.Torrent, opts DescriptionImageCacheOptions, results map[string]interface{}, errors *[]map[string]interface{}) error {
	if t.TorrentURL == "" {
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}

	// Bound the work for a single torrent so a slow/dead image host cannot stall
	// the whole job indefinitely.
	torrentCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	key := TorrentKey(t)

	// Inspect any existing cover so we can (a) skip rows already upgraded to a
	// TPDB/StashDB cover and (b) cheaply upgrade NFO/description rows that have
	// since been resolved - without re-scraping the detail page.
	hasCover, existingSource, existingMetaID := r.existingCoverState(torrentCtx, key)
	if !opts.ForceRefresh && hasCover && isUpgradedCoverSource(existingSource) {
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}

	if !opts.ForceRefresh && hasCover && existingMetaID != "" {
		// Reuse-only upgrade path: no detail scrape, no API call.
		if merged := r.loadSharedMeta(torrentCtx, opts.meta.store, existingMetaID); merged != nil && merged.Poster != "" {
			if r.persistCover(torrentCtx, key, merged.Poster, coverSourceOf(merged), merged.Description, existingMetaID, results, errors, t.Name) {
				results["coverUpgraded"] = results["coverUpgraded"].(int) + 1
				results["metaReused"] = results["metaReused"].(int) + 1
				if merged.Description != "" {
					results["descUpdated"] = results["descUpdated"].(int) + 1
				}
				log.Printf("[DescImageCache] Upgraded cover via %s for: %s", coverSourceOf(merged), t.Name)
			}
		} else {
			results["skipped"] = results["skipped"].(int) + 1
		}
		return nil
	}

	details, err := r.scrapers.GetTorrentDetails(torrentCtx, scraperWebsite, t.TorrentURL)
	if err != nil || details == nil {
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}

	website := t.Website
	if website == "" {
		website = scraperWebsite
	}
	detailURL := t.TorrentURL
	infoHash := details.InfoHash
	if infoHash == "" {
		magnet := details.MagnetLink
		if magnet == "" {
			magnet = t.MagnetLink
		}
		infoHash = extractInfoHash(magnet)
	}
	metaID := StableMetaID(website, detailURL, infoHash)

	// 1) Reuse already-resolved metadata (free); 2) inline-resolve if still missing.
	var merged *SharedMeta
	if metaID != "" {
		merged = r.loadSharedMeta(torrentCtx, opts.meta.store, metaID)
		if merged != nil && merged.Poster != "" {
			results["metaReused"] = results["metaReused"].(int) + 1
		} else if resolved := opts.meta.resolveInline(torrentCtx, r, metaID, t.Name, detailURL, results); resolved != nil {
			merged = MergeShared(merged, resolved)
			if merged != nil && merged.Poster != "" {
				results["metaResolved"] = results["metaResolved"].(int) + 1
			}
		}
	}

	// Prefer the TPDB/StashDB poster as the cover when we resolved one.
	if merged != nil && merged.Poster != "" {
		if r.persistCover(torrentCtx, key, merged.Poster, coverSourceOf(merged), merged.Description, metaID, results, errors, t.Name) {
			results["cached"] = results["cached"].(int) + 1
			if hasCover && !isUpgradedCoverSource(existingSource) {
				results["coverUpgraded"] = results["coverUpgraded"].(int) + 1
			}
			if opts.ForceRefresh {
				results["replaced"] = results["replaced"].(int) + 1
			}
			if merged.Description != "" {
				results["descUpdated"] = results["descUpdated"].(int) + 1
			}
			log.Printf("[DescImageCache] Cached %s cover for: %s", coverSourceOf(merged), t.Name)
		}
		return nil
	}

	// Fallback: NFO cover, else a random one of the first 3 description images
	// (mirrors the Node backend behaviour). Queue the entry so the MetaEnricher
	// can resolve a match and a later run can upgrade the cover.
	rawURL, coverSrc := pickFallbackCover(details)
	if rawURL == "" {
		r.enqueueDescMeta(t.Name, detailURL, website, infoHash)
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}

	results["imagesFound"] = results["imagesFound"].(int) + 1

	// Resolve image-hosting viewer pages to direct image URLs.
	directURL, err := images.ExtractDirectImageURL(torrentCtx, r.httpClient, rawURL)
	if err != nil {
		log.Printf("[DescImageCache] Failed to resolve cover URL %s: %v", rawURL, err)
	}
	if directURL == "" {
		directURL = rawURL
	}

	finalURL := higherResolutionURL(directURL)
	if finalURL != directURL {
		if !validateImageURL(torrentCtx, r.httpClient, finalURL) {
			finalURL = reMdExt.ReplaceAllString(directURL, "$1")
		}
	} else {
		finalURL = reMdExt.ReplaceAllString(directURL, "$1")
	}

	if r.persistCover(torrentCtx, key, finalURL, coverSrc, "", metaID, results, errors, t.Name) {
		results["cached"] = results["cached"].(int) + 1
		if opts.ForceRefresh {
			results["replaced"] = results["replaced"].(int) + 1
		}
		log.Printf("[DescImageCache] Cached cover for: %s", t.Name)
	}
	r.enqueueDescMeta(t.Name, detailURL, website, infoHash)
	return nil
}

// existingCoverState reports whether a cover exists for key and its stored
// cover source and meta id.
func (r *Runner) existingCoverState(ctx context.Context, key string) (hasCover bool, source, metaID string) {
	e, err := r.storage.GetCoverImageByKey(ctx, key)
	if err != nil || e == nil {
		return false, "", ""
	}
	if e.CoverSource != nil {
		source = *e.CoverSource
	}
	if e.MetaID != nil {
		metaID = *e.MetaID
	}
	return e.PixhostURL != "", source, metaID
}

// persistCover writes an enriched cover row, recording a failure into results on
// error. It returns true when the cover was stored.
func (r *Runner) persistCover(ctx context.Context, key, imageURL, source, description, metaID string, results map[string]interface{}, errors *[]map[string]interface{}, name string) bool {
	if err := r.storage.SetCoverImageEnriched(ctx, key, imageURL, true, source, description, metaID); err != nil {
		results["failed"] = results["failed"].(int) + 1
		pushJobError(errors, descMaxErrors, map[string]interface{}{"torrent": name, "error": err.Error()})
		return false
	}
	return true
}

func pushJobError(errors *[]map[string]interface{}, max int, entry map[string]interface{}) {
	if len(*errors) < max {
		*errors = append(*errors, entry)
	}
}

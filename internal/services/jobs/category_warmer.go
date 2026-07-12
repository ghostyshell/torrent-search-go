package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/metadata"
)

const (
	categoryScenesPerPage = 40
	categoryMaxScenes     = 25
	categoryMaxMatches    = 20
	categoryBackendPace   = 150 * time.Millisecond
	cat4K                 = "507"
	cat1080p              = "505"
	categoryScraper       = "piratebay"
)

// CategoryWarmer pre-builds per-(source, category) Stremio meta lists in Redis.
func (r *Runner) CategoryWarmer(ctx context.Context) (map[string]interface{}, error) {
	if r.redis == nil {
		return nil, fmt.Errorf("redis not configured")
	}
	if r.cfg == nil {
		return nil, fmt.Errorf("config not available")
	}

	tpdbKey := r.cfg.Metadata.TPDBAPIKey
	stashKey := r.cfg.Metadata.StashDBAPIKey
	if tpdbKey == "" && stashKey == "" {
		return map[string]interface{}{
			"success": true,
			"skipped": true,
			"reason":  "no TPDB_API_KEY or STASHDB_API_KEY",
		}, nil
	}

	store := newAddonRedisStore(r.redis)
	tpdb := metadata.NewTPDBClient(r.cfg.Metadata.TPDBAPIURL, tpdbKey)
	stash := metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, stashKey)

	results := map[string]interface{}{
		"success": true,
	}

	for _, source := range []string{"tpdb", "stashdb"} {
		if source == "tpdb" && tpdbKey == "" {
			continue
		}
		if source == "stashdb" && stashKey == "" {
			continue
		}

		warmed := 0
		entries := 0
		for _, cat := range OrderedCategories() {
			if err := ctx.Err(); err != nil {
				return results, err
			}
			n, err := r.warmCategory(ctx, source, cat, store, tpdb, stash)
			if err != nil {
				continue
			}
			if n > 0 {
				warmed++
				entries += n
			}
		}
		results[source+"Categories"] = warmed
		results[source+"Entries"] = entries
	}

	return results, nil
}

func (r *Runner) warmCategory(
	ctx context.Context,
	source string,
	cat CategoryDef,
	store *addonRedisStore,
	tpdb *metadata.TPDBClient,
	stash *metadata.StashDBClient,
) (int, error) {
	var scenes []metadata.Scene
	var err error

	switch source {
	case "stashdb":
		scenes, err = stash.FetchScenes(ctx, store.stashTagCache(), cat.StashTag, categoryScenesPerPage)
	case "tpdb":
		scenes, err = tpdb.FetchScenes(ctx, cat.TPDBQuery, categoryScenesPerPage)
	default:
		return 0, nil
	}
	if err != nil || len(scenes) == 0 {
		return 0, err
	}

	limit := categoryMaxScenes
	if len(scenes) < limit {
		limit = len(scenes)
	}

	metas := make([]StremioMetaPreview, 0, categoryMaxMatches)
	for i := 0; i < limit && len(metas) < categoryMaxMatches; i++ {
		scene := scenes[i]
		torrent, err := r.findMatchingTorrent(ctx, scene, 1)
		if err != nil || torrent == nil {
			continue
		}

		infoHash := extractInfoHash(torrent.MagnetLink)
		if infoHash == "" {
			continue
		}

		id := encodeItemID(infoHash, torrent.Name, torrent.TorrentURL, "hiddenbay", torrent.TorrentURL)
		_ = store.SetTorrent(ctx, id, TorrentRecord{
			ID:         id,
			Title:      torrent.Name,
			InfoHash:   infoHash,
			MagnetLink: torrent.MagnetLink,
			TorrentURL: torrent.TorrentURL,
			DetailURL:  torrent.TorrentURL,
			Website:    "hiddenbay",
			Seeders:    torrent.Seeders,
		})

		year := yearFromDate(scene.Date)
		has, _ := store.HasSharedMeta(ctx, source, infoHash)
		if !has {
			_ = store.SetSharedMeta(ctx, source, infoHash, SharedMeta{
				Title:       scene.Title,
				Description: scene.Description,
				Poster:      scene.Poster,
				Background:  scene.Poster,
				Year:        year,
				Cast:        scene.Performers,
				Tags:        scene.Tags,
				Source:      source,
			})
		}

		meta := StremioMetaPreview{
			ID:          id,
			Type:        "Porn",
			Name:        scene.Title,
			Description: scene.Description,
			ReleaseInfo: year,
			PosterShape: "landscape",
		}
		if scene.Poster != "" {
			meta.Poster = scene.Poster
			meta.Background = scene.Poster
		}
		metas = append(metas, meta)
	}

	if len(metas) > 0 {
		_ = store.SetCategoryMetas(ctx, source, cat.Slug, metas)
	}
	return len(metas), nil
}

func (r *Runner) findMatchingTorrent(ctx context.Context, scene metadata.Scene, maxPages int) (*models.Torrent, error) {
	if maxPages < 1 {
		maxPages = 1
	}
	candidate := metadata.MatchCandidate{
		Title:      scene.Title,
		Studio:     scene.Studio,
		Performers: scene.Performers,
		Date:       scene.Date,
	}
	for _, query := range tpbQueries(scene) {
		for page := 1; page <= maxPages; page++ {
			torrents, err := r.scrapers.Search(ctx, categoryScraper, query, page, models.SearchOptions{Sort: "7"})
			if err := sleepCtx(ctx, categoryBackendPace); err != nil {
				return nil, err
			}
			if err != nil {
				break
			}
			for _, t := range torrents {
				if metadata.VerifyMatch(metadata.ParseRelease(t.Name), candidate) {
					return &t, nil
				}
			}
			if len(torrents) == 0 {
				break
			}
		}
	}
	return nil, nil
}

func sceneQuery(scene metadata.Scene) string {
	if len(scene.Performers) > 0 {
		return scene.Performers[0]
	}
	words := strings.Fields(scene.Title)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, " ")
}

// tpbQueries returns the search queries to try for a TPB scene match, in order.
// Two patterns:
//   - Title-based: the first-billed performer (TPB porn torrents are usually titled
//     by performer), then the first few title words for torrents titled by scene.
//   - Studio/date/performer-based: TPB 4K/1080p rips are often named
//     "SiteName YYYY-MM-DD Performer" with no scene title, so the title-based
//     queries miss them when the torrent's performer spelling differs from the
//     canonical TPDB name or the date-only naming is used. "studio + date" surfaces
//     date-named rips independent of performer spelling; "studio + first name"
//     looses the performer match for first-name-only or variant spellings. The
//     full "studio + performer" form is skipped - it is a strict subset of the
//     performer query above (TPB AND-search), so it surfaces nothing new.
//
// The piratebay scraper's Search hits all categories (it hardcodes cat 0), so
// recall is widened by page depth + query choice, not category. Precision is
// unchanged: VerifyMatch gates every candidate (it already accepts
// studio+date+performer without title overlap, so pattern B verifies there).
func tpbQueries(scene metadata.Scene) []string {
	var qs []string
	seen := map[string]bool{}
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q != "" && !seen[q] {
			seen[q] = true
			qs = append(qs, q)
		}
	}
	if len(scene.Performers) > 0 {
		add(scene.Performers[0])
	}
	words := strings.Fields(scene.Title)
	if len(words) > 4 {
		words = words[:4]
	}
	add(strings.Join(words, " "))
	// Pattern B: studio/date/performer-named 4K/1080p rips. Recall-only - VerifyMatch
	// gates on studio+date+performer, so a wrong-date or wrong-performer hit is rejected.
	if scene.Studio != "" {
		if scene.Date != "" {
			add(scene.Studio + " " + scene.Date)
		}
		if len(scene.Performers) > 0 {
			add(scene.Studio + " " + firstNameOf(scene.Performers[0]))
		}
	}
	return qs
}

// firstNameOf returns the first whitespace-separated token of a performer name -
// the loose performer term for the "studio + first name" pattern-B query. TPB 4K
// rips often use a single name or a variant spelling, so the full canonical name
// (the pattern-A performer query) misses them.
func firstNameOf(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return name
}

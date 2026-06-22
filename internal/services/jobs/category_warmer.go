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
		torrent, err := r.findMatchingTorrent(ctx, scene)
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

func (r *Runner) findMatchingTorrent(ctx context.Context, scene metadata.Scene) (*models.Torrent, error) {
	query := sceneQuery(scene)
	if query == "" {
		return nil, nil
	}

	candidate := metadata.MatchCandidate{
		Title:      scene.Title,
		Studio:     scene.Studio,
		Performers: scene.Performers,
		Date:       scene.Date,
	}

	for _, category := range []string{cat4K, cat1080p} {
		torrents, err := r.scrapers.Search(ctx, categoryScraper, query, 1, models.SearchOptions{
			Category: category,
			Sort:     "7",
		})
		if err := sleepCtx(ctx, categoryBackendPace); err != nil {
			return nil, err
		}
		if err != nil {
			continue
		}
		for _, t := range torrents {
			parsed := metadata.ParseRelease(t.Name)
			if metadata.VerifyMatch(parsed, candidate) {
				return &t, nil
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

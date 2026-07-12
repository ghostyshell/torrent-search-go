package stremio

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"torrent-search-go/internal/services/jobs"
	prmodels "torrent-search-go/pkg/models"
)

// ServeMeta returns full Stremio metadata for supported item ids.
func (h *Handler) ServeMeta(ctx context.Context, cfg Config, contentType, id string) (MetaResponse, error) {

	if strings.HasPrefix(id, "porndb:") || strings.HasPrefix(id, "stash:") {
		meta, err := h.serveTPDBMeta(ctx, id)
		if err != nil || meta == nil {
			return MetaResponse{Meta: nil}, err
		}
		// Always "Porn": catalog items must match meta type or stremio-core
		// rejects the detail page ("No metadata found"), even if the client
		// requested /meta/movie/… from stale catalog rows typed "movie".
		meta.Type = "Porn"
		return MetaResponse{Meta: meta}, nil
	}

	if strings.HasPrefix(id, "hmm-") {
		meta, err := h.serveHentaiMeta(ctx, id)
		if err != nil || meta == nil {
			return MetaResponse{Meta: nil}, err
		}
		meta.Type = "series"
		return MetaResponse{Meta: meta}, nil
	}

	// Tube sources (scraped direct, no TPDB/StashDB): pvz:{slug} / fpv:{id} and
	// the later ypv: / wpt: / pec: / phd: / p4d: / hqp: ids. serveTubeMeta stamps
	// Type="Porn". h.TubeSources is nil only in bare-handler tests.
	if h.TubeSources != nil {
		if src := h.TubeSources.LookupByIDPrefix(id); src != nil {
			meta, err := h.serveTubeMeta(ctx, src, id)
			if err != nil || meta == nil {
				return MetaResponse{Meta: nil}, err
			}
			return MetaResponse{Meta: meta}, nil
		}
	}

	// Legacy "hs:"/"hse-"/"hs-" ids came from the external HentaiStream worker
	// and do not map to Mongo docs; the worker proxy was removed in Phase C, so
	// they now return no metadata (old bookmarks get "No metadata found").

	if strings.HasPrefix(id, "sc:") {
		meta, err := h.serveStripchatMeta(ctx, id)
		if err != nil {
			return MetaResponse{Meta: nil}, err
		}
		if meta == nil {
			return MetaResponse{Meta: nil}, nil
		}
		meta.Type = "Porn"
		return MetaResponse{Meta: meta}, nil
	}

	if strings.HasPrefix(id, "jstrg:") {
		// Compact studio catalog entry: one id encodes the 4K + 1080p variants
		// of a scene. Build the detail-page meta from the representative (first)
		// member, but stamp the original jstrg: id so stremio-core's id match
		// holds. Streams are resolved by the Node edge from the full group.
		group := DecodeGroupID(id)
		if len(group) == 0 {
			return MetaResponse{Meta: nil}, nil
		}
		rep := group[0]
		repID := EncodeItemID(TorrentRecord{InfoHash: rep.H, Title: rep.T, TorrentURL: rep.U, Website: rep.W, DetailURL: rep.D, Quality: rep.Q})
		return h.serveJstrmMeta(ctx, cfg, contentType, repID, id)
	}

	if !strings.HasPrefix(id, "jstrm:") {
		return MetaResponse{Meta: nil}, nil
	}

	return h.serveJstrmMeta(ctx, cfg, contentType, id, id)
}

// serveJstrmMeta builds full Stremio metadata for a single jstrm: id. id is the
// record to decode and look up; stampID is the id written to Meta.ID (for
// jstrg: group entries this is the original group id, not the representative's
// jstrm: id, so stremio-core's id match holds).
func (h *Handler) serveJstrmMeta(ctx context.Context, cfg Config, contentType, id, stampID string) (MetaResponse, error) {
	rec := DecodeItemID(id)
	if rec == nil {
		return MetaResponse{Meta: nil}, nil
	}

	store := newRedisStore(h.Redis)
	stored, _ := store.getTorrent(ctx, id)

	torrent := TorrentRecord{
		Title:      rec.T,
		InfoHash:   rec.H,
		TorrentURL: rec.U,
		Website:    rec.W,
		DetailURL:  rec.D,
	}
	if stored != nil {
		torrent = *stored
	}

	title, year := ParseTorrentTitle(torrent.Title)
	if title == "" {
		title = rec.T
	}

	website := rec.W
	if website == "" && stored != nil {
		website = stored.Website
	}
	detailURL := rec.D
	if detailURL == "" && stored != nil {
		detailURL = stored.DetailURL
	}
	infoHash := rec.H
	if infoHash == "" && stored != nil {
		infoHash = stored.InfoHash
	}

	metaID := StableMetaID(website, detailURL, infoHash)
	var merged *jobs.SharedMeta
	switch website {
	case "pornrips":
		merged = h.loadPornripsMeta(ctx, store, metaID)
	case "sukebei":
		merged = h.loadStashMeta(ctx, cfg, store, metaID, torrent.Title, detailURL)
	default:
		merged = h.loadMergedMeta(ctx, cfg, store, metaID, torrent.Title, detailURL)
	}

	// PornRips is Mongo-only: never enqueue it for the on-demand MetaEnricher
	// (the background PornripsSync job is the sole metadata populator); other
	// sources enqueue a live-probe fallback as before.
	if h.MetaEnqueuer != nil && website != "pornrips" {
		h.MetaEnqueuer(ctx, []jobs.MetaEnqueueItem{{
			Title:     torrent.Title,
			DetailURL: detailURL,
			Website:   website,
			InfoHash:  infoHash,
		}})
	}

	// For a PornRips item with no enriched shared_meta, fall back to the durable
	// Mongo entry's WP featured image / dotted title / date. No live WP/TPDB/Stash
	// probe on the request path.
	var entry *prmodels.PornripsEntry
	if website == "pornrips" && !mergedHasPoster(merged) && h.Pornrips != nil {
		if slug := PornripsSlug(detailURL); slug != "" {
			if cached, ok := store.getPornripsMeta(ctx, slug); ok {
				entry = cached
			} else {
				entry, _ = h.Pornrips.GetPornripsEntryBySlug(ctx, slug)
				if entry != nil {
					_ = store.setPornripsMeta(ctx, slug, entry)
				}
			}
		}
	}

	poster := ""
	if merged != nil {
		poster = merged.Poster
	}
	if poster == "" && torrent.CoverImage != "" {
		poster = torrent.CoverImage
	}
	if poster == "" && website != "pornrips" {
		poster = h.resolveCover(ctx, torrent)
	}
	if poster == "" && entry != nil && entry.WpPoster != "" {
		poster = entry.WpPoster
	}
	if poster == "" {
		poster = placeholderPoster(title)
	}

	name := title
	if merged != nil && merged.Title != "" {
		name = merged.Title
	}
	// PornRips WP titles are dotted release filenames; render a readable name
	// from the raw title when no enriched shared_meta supplied one.
	if website == "pornrips" && (merged == nil || merged.Title == "") {
		name = strings.TrimSpace(strings.ReplaceAll(torrent.Title, ".", " "))
	}
	if name == "" {
		name = torrent.Title
	}
	if name == "" {
		name = "Unknown"
	}

	release := year
	if merged != nil && merged.Year != "" {
		release = merged.Year
	}
	if release == "" && entry != nil && entry.Date != "" {
		release = entry.Date
	}

	desc := buildMetaDescription(torrent)
	if merged != nil && merged.Description != "" {
		desc = merged.Description
	}

	background := poster
	if merged != nil && merged.Background != "" {
		background = merged.Background
	}

	meta := &Meta{
		ID:          stampID,
		Type:        contentType,
		Name:        name,
		ReleaseInfo: release,
		Poster:      poster,
		Background:  background,
		Description: desc,
	}
	// Surface the enriched tags as Stremio genres and the cast as Stremio search
	// links, mirroring enrichedSceneToMeta / serveTPDBMetaLive (which also feed
	// tags into Genres). merged is the SharedMeta from loadPornripsMeta ->
	// MergeShared, which keeps Tags and Genres as separate unions; we surface
	// Tags (TPDB/Stash scene tags) as genres, matching the TPDB meta path.
	// Without this the pornrips meta page shows no genres/cast even for
	// fully-enriched entries.
	if merged != nil {
		meta.Genres = merged.Tags
		meta.Links = metaLinks(merged.Cast, merged.Tags)
	}
	if website == "sukebei" {
		meta.PosterShape = "landscape"
	}
	if rec.U != "" {
		meta.Website = rec.U
	}
	return MetaResponse{Meta: meta}, nil
}

func buildMetaDescription(t TorrentRecord) string {
	parts := make([]string, 0, 5)
	if qt := QualityTag(t.Title); qt != "" {
		parts = append(parts, qt)
	}
	if t.Size != "" {
		parts = append(parts, t.Size)
	}
	if t.Seeders > 0 {
		parts = append(parts, strconv.Itoa(t.Seeders)+" seeders")
	}
	if t.Leechers > 0 {
		parts = append(parts, strconv.Itoa(t.Leechers)+" leechers")
	}
	if t.Indexer != "" {
		parts = append(parts, "via "+t.Indexer)
	}
	if len(parts) == 0 {
		return t.Title
	}
	return strings.Join(parts, " | ")
}

func placeholderPoster(title string) string {
	text := title
	if len(text) > 28 {
		text = text[:25] + "..."
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="450">
    <defs>
      <linearGradient id="g" x1="0%%" y1="0%%" x2="100%%" y2="100%%">
        <stop offset="0%%" stop-color="#1a1625"/>
        <stop offset="100%%" stop-color="#0b0b10"/>
      </linearGradient>
    </defs>
    <rect width="300" height="450" fill="url(#g)"/>
    <rect x="20" y="20" width="260" height="410" rx="12" fill="none" stroke="#d946ef" stroke-width="2"/>
    <text x="150" y="200" font-family="sans-serif" font-size="14" fill="#a89bb8" text-anchor="middle">No cover found</text>
    <text x="150" y="240" font-family="sans-serif" font-size="12" fill="#f3f0f7" text-anchor="middle">%s</text>
  </svg>`, text)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

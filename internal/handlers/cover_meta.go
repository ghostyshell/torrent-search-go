package handlers

import (
	"context"
	"regexp"
	"strings"

	"torrent-search-go/internal/models"
	pkgmodels "torrent-search-go/pkg/models"
)

var (
	pornripsSlugRE = regexp.MustCompile(`(?i)pornrips\.to/([^/?#]+)`)
	magnetHashRE   = regexp.MustCompile(`(?i)urn:btih:([a-f0-9]{40})`)
	sdbImageRE     = regexp.MustCompile(`(?i)stashdb|cdn\.stash`)
)

// isUpgradedCoverSource reports whether a stored cover came from TPDB/StashDB
// rather than an NFO or torrent-description fallback.
func isUpgradedCoverSource(s string) bool {
	return s != "" && s != "nfo" && s != "description"
}

func stableMetaID(website, detailURL, infoHash string) string {
	if website == "pornrips" {
		if m := pornripsSlugRE.FindStringSubmatch(detailURL); len(m) > 1 {
			return "pr:" + m[1]
		}
		return ""
	}
	return strings.ToLower(infoHash)
}

func magnetInfoHash(magnet string) string {
	if magnet == "" {
		return ""
	}
	if m := magnetHashRE.FindStringSubmatch(magnet); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

func metaIDForTorrent(t *models.Torrent, website string) string {
	infoHash := magnetInfoHash(t.MagnetLink)
	return stableMetaID(website, t.TorrentURL, infoHash)
}

type mergedCoverMeta struct {
	poster string
	source string
}

func loadMergedCoverMeta(ctx context.Context, storage *StorageProvider, metaID string) mergedCoverMeta {
	if metaID == "" || storage == nil {
		return mergedCoverMeta{}
	}
	tp, stash, err := storage.GetSharedMetaPair(ctx, metaID)
	if err != nil {
		return mergedCoverMeta{}
	}
	poster := pickMergedPoster(tp, stash)
	if poster == "" {
		return mergedCoverMeta{}
	}
	return mergedCoverMeta{poster: poster, source: mergeCoverSource(tp, stash)}
}

func pickMergedPoster(tp, stash *pkgmodels.SharedMetaPayload) string {
	tpPoster := ""
	stashPoster := ""
	if tp != nil {
		tpPoster = strings.TrimSpace(tp.Poster)
	}
	if stash != nil {
		stashPoster = strings.TrimSpace(stash.Poster)
	}
	if tpPoster == "" {
		return stashPoster
	}
	if stashPoster == "" {
		return tpPoster
	}
	return pickSharedImageURL(tpPoster, stashPoster)
}

func pickSharedImageURL(a, b string) string {
	aIsSdb := sdbImageRE.MatchString(a)
	bIsSdb := sdbImageRE.MatchString(b)
	if aIsSdb && !bIsSdb {
		return a
	}
	if bIsSdb && !aIsSdb {
		return b
	}
	if len(a) >= len(b) {
		return a
	}
	return b
}

func mergeCoverSource(tp, stash *pkgmodels.SharedMetaPayload) string {
	a, b := "", ""
	if tp != nil {
		a = strings.TrimSpace(tp.Source)
	}
	if stash != nil {
		b = strings.TrimSpace(stash.Source)
	}
	switch {
	case a != "" && b != "" && a != b:
		return a + "+" + b
	case a != "":
		return a
	default:
		return b
	}
}

type coverResolveInput struct {
	row    *pkgmodels.ImageRow
	metaID string
}

type coverResolveResult struct {
	url        string
	tpdbURL    string
	detailsURL string
	source     string
	upgraded   bool
}

// resolveCover picks the best cover URL. Priority for the primary url:
// manual (pixhost_url) > TPDB/StashDB (shared_meta or tpdb_url) > details scrape.
// tpdbURL and detailsURL are always populated from their respective slots so
// callers can expose fallback covers to the UI.
func resolveCover(ctx context.Context, storage *StorageProvider, in coverResolveInput) coverResolveResult {
	rowSource := ""
	if in.row != nil && in.row.CoverSource != nil {
		rowSource = *in.row.CoverSource
	}

	metaID := in.metaID
	if metaID == "" && in.row != nil && in.row.MetaID != nil {
		metaID = *in.row.MetaID
	}

	merged := loadMergedCoverMeta(ctx, storage, metaID)

	res := coverResolveResult{}

	// Populate dedicated slots from the row.
	if in.row != nil {
		if in.row.TpdbURL != nil && *in.row.TpdbURL != "" {
			res.tpdbURL = *in.row.TpdbURL
		}
		if in.row.DetailsURL != nil && *in.row.DetailsURL != "" {
			res.detailsURL = *in.row.DetailsURL
		}
	}

	// Preferred TPDB source: shared_meta trumps stored row.
	if merged.poster != "" && res.tpdbURL == "" {
		res.tpdbURL = merged.poster
	}

	// Primary URL resolution: manual > tpdb > details.
	isManual := rowSource == "manual"
	if isManual && in.row != nil && in.row.PixhostURL != "" {
		res.url = in.row.PixhostURL
		res.source = rowSource
		return res
	}
	if merged.poster != "" {
		upgraded := in.row != nil && in.row.PixhostURL != "" && !isUpgradedCoverSource(rowSource)
		res.url = merged.poster
		res.source = merged.source
		res.upgraded = upgraded
		return res
	}
	if in.row != nil && in.row.PixhostURL != "" && isUpgradedCoverSource(rowSource) {
		res.url = in.row.PixhostURL
		res.source = rowSource
		return res
	}
	if res.tpdbURL != "" {
		res.url = res.tpdbURL
		return res
	}
	if res.detailsURL != "" {
		res.url = res.detailsURL
		return res
	}
	if in.row != nil && in.row.PixhostURL != "" {
		res.url = in.row.PixhostURL
		res.source = rowSource
		return res
	}
	return res
}

func metaIDFromTorrentMap(m map[string]interface{}) string {
	website := torrentSourceFromMap(m)
	detailURL := mapStrField(m, "Url", "url")
	infoHash := magnetInfoHash(mapStrField(m, "Magnet", "magnet"))
	return stableMetaID(website, detailURL, infoHash)
}

func imgRowOriginalURL(row *pkgmodels.ImageRow) *string {
	if row == nil {
		return nil
	}
	return row.OriginalURL
}

func (p *StorageProvider) upgradeCoverFromMeta(ctx context.Context, key, poster, source, metaID string) {
	if p == nil || key == "" || poster == "" {
		return
	}
	_ = p.SetCoverImageEnriched(ctx, key, poster, true, source, "", metaID)
}

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
	url      string
	source   string
	upgraded bool
}

// resolveCover picks the best cover URL, preferring TPDB/StashDB metadata over
// stored description/NFO fallbacks - matching the Stremio addon catalog path.
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

	if in.row != nil && in.row.PixhostURL != "" && isUpgradedCoverSource(rowSource) {
		return coverResolveResult{url: in.row.PixhostURL, source: rowSource}
	}
	if merged.poster != "" {
		upgraded := in.row != nil && in.row.PixhostURL != "" && !isUpgradedCoverSource(rowSource)
		return coverResolveResult{url: merged.poster, source: merged.source, upgraded: upgraded}
	}
	if in.row != nil && in.row.PixhostURL != "" {
		return coverResolveResult{url: in.row.PixhostURL, source: rowSource}
	}
	return coverResolveResult{}
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

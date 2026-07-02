package stremio

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	prmodels "torrent-search-go/pkg/models"
)

type pornripsParams struct {
	Website  string
	Category string
	Query    string
}

var pornripsQualityRE = regexp.MustCompile(`(?i)\b(?:480p|540p|720p|1080p|1440p|2160p|4k|uhd|hevc|x265|x264|h\.?265|h\.?264|prt)\b`)

// prTagAliases maps a pr_tag category (NormToken form) to the real tags_norm
// tokens TPDB/Stash emit for that concept. The pr_tag filter matches tags_norm
// exactly, so a category whose content lives under compound TPDB tokens (milf30,
// teen1822, tattoos, ...) would otherwise show zero entries. Tokens are the
// positive-concept forms only (e.g. "tattoos" but not "tattoosnone"/"notattoos").
// Curated from the on-disk vocabulary audit 2026-06-29 (cmd/praudit); refresh if
// the TPDB genre taxonomy shifts. Categories not listed fall through to the bare
// NormToken (the original exact-match behaviour).
var prTagAliases = map[string][]string{
	"milf":               {"milf30", "bustymilf", "nudemilf", "over40milf", "oldermilf", "oldmilf", "petitemilf"},
	"teen":                {"teen1822", "teengirl1822"},
	"mature":              {"mature30", "puremature", "nudemature", "matureanalporn", "maturecouples", "maturepussy", "maturemasturbating", "maturehandjob", "maturelesbian", "maturestud"},
	"latina":              {"latinawoman", "ethnicitylatina", "latinaass", "latinasolo", "latinadebut", "latinaperformer", "latinamericanaccent", "afrolatina", "miamilatina", "danylatina"},
	// ebony: blackwoman is the real ebony token (1486); bare "black" is ambiguous (black
	// hair/stockings/eyes), "blackmail" is crime fantasy - both excluded. Curated from the
	// pornrips_entries tags_norm audit 2026-06-30.
	"ebony":               {"blackwoman", "ebonyonebonysex", "blackonblack"},
	"blonde":              {"blondhair", "blondehairfemale", "sexyblondeporn", "interracialblonde"},
	// redhead: redhairfemale (18639) + redhair (4340); the prior "naturalredheads" alias
	// was dead (not in the pornrips vocabulary). coloredhair/redlipstick/stockingsred excluded.
	"redhead":             {"redhairfemale", "redhair"},
	"petite":              {"bodypetite", "petitemilf", "petitegirldestroyed", "petitedemolished", "petitem"},
	"tattoo":              {"tattoos", "tattooedwoman", "heavilytattooed", "tattoospiercings", "tattooedman", "tattooedbabes", "tattooedtrans", "mytattoogirls", "armtattoo", "legtattoo", "backtattoo", "chesttattoo", "facetattoo", "shouldertattoo", "hiptattoo", "sidetattoo", "asstattoo", "stomachtattoo", "underbreasttattoo", "necktattoo", "fingertattoo", "handtattoo", "headtattoo", "throattattoo", "pussytattoo", "nippletattoo", "textualtattoo", "intimatetattoos", "cumontattoo", "degradingtattoo", "animetattoogirl"},
	"doublepenetration":   {"doublepenetrationdp", "doublepenetrationasspussy", "doublepenetrationmouthpussy", "doublepenetrationwithtoysorfigures"},
	"public":              {"publicnudity", "publicsex", "publicagent", "publicpickups", "publicporn", "publicdisplayofaffection", "publicteasing", "publicplace", "publicanal", "publictransports", "sexpublic"},
	"cuckold":             {"cuckolding", "cuckoldpov", "cuckoldhusband", "cuckoldhandholding"},
	"feet":                {"cumonfeet", "hdfeetporn", "visiblefeet", "dirtyfeet", "closeupfeet", "oilyfeet", "passionforfeet", "feetinfaceduringsex", "feetinfaceduringblowjob", "feetinmouth", "feetsniffing", "suckinghisfeet"},
	// stepfamily/pissing/roughsex: bare NormToken is dead in the pornrips vocabulary; these
	// expand to the compound tokens the enrich sweep actually writes. Curated from the
	// 2026-06-30 tags_norm audit (false positives like blackmail/blackhair excluded).
	"stepfamily":          {"stepmother", "stepdaughter", "stepsister", "stepdad", "stepson", "stepbrother", "stepsiblings", "stepaunt", "stepuncle", "stepgrandmother", "stepgrandfather", "stepniece", "stepnephew", "stepcousin"},
	"pissing":             {"pissinmouth", "pissdrinking", "pissinass", "pissplay", "drinkingabowlofpiss", "pissinthroat", "pissinpussy", "pissongenitals", "pissonface", "drowninginpiss", "pisslover", "pissswapping", "severalstreamsofpissinmouth", "pissreceptacle"},
	"roughsex":            {"rough"},
}

// prTagTokens resolves a pr_tag genre to the tags_norm tokens to query. Aliased
// categories expand to their compound-token set; everything else falls back to
// the bare normalized token (the original exact-match behaviour).
func prTagTokens(genre string) []string {
	n := prmodels.NormToken(genre)
	if n == "" {
		return nil
	}
	if toks, ok := prTagAliases[n]; ok {
		return toks
	}
	return []string{n}
}

// PornripsTagTokens is the exported form of prTagTokens for read-only tooling
// (cmd/tpbaudit) so it can replicate the deployed pr_tag query without
// duplicating the pornrips alias map.
func PornripsTagTokens(genre string) []string { return prTagTokens(genre) }

func getPornripsParams(catalogID, genre, searchQ string) *pornripsParams {
	if !strings.HasPrefix(catalogID, "pr_") {
		return nil
	}
	g := strings.TrimSpace(genre)
	if g == "All" {
		g = ""
	}
	s := strings.TrimSpace(searchQ)
	query := ""
	switch catalogID {
	case "pr_search":
		query = s
	default:
		parts := make([]string, 0, 2)
		if g != "" {
			parts = append(parts, g)
		}
		if s != "" {
			parts = append(parts, s)
		}
		query = strings.Join(parts, " ")
	}
	return &pornripsParams{Website: "pornrips", Category: "all", Query: query}
}

func dedupePornripsCatalog(torrents []catalogTorrent) []catalogTorrent {
	keyOf := func(t catalogTorrent) string {
		title := strings.ToLower(t.Title)
		title = pornripsQualityRE.ReplaceAllString(title, "")
		title = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(title, " ")
		return strings.TrimSpace(title)
	}
	rank := func(t catalogTorrent) int {
		title := t.Title
		switch {
		case regexp.MustCompile(`(?i)\b(?:2160p|4k|uhd)\b`).MatchString(title):
			return 3
		case regexp.MustCompile(`(?i)\b(?:1080p|1440p)\b`).MatchString(title):
			return 2
		default:
			return 1
		}
	}

	seen := make(map[string]catalogTorrent)
	order := make([]string, 0, len(torrents))
	for _, t := range torrents {
		k := keyOf(t)
		if k == "" {
			k = t.DetailURL
		}
		if k == "" {
			k = t.Title
		}
		if k == "" {
			continue
		}
		if existing, ok := seen[k]; ok {
			if rank(t) > rank(existing) {
				seen[k] = t
			}
			continue
		}
		seen[k] = t
		order = append(order, k)
	}
	out := make([]catalogTorrent, 0, len(order))
	for _, k := range order {
		out = append(out, seen[k])
	}
	return out
}

func (h *Handler) servePornripsCatalog(ctx context.Context, cfg Config, catalogID, contentType, genre, searchQ string, skip int) (CatalogResponse, error) {
	params := getPornripsParams(catalogID, genre, searchQ)
	if params == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if catalogID == "pr_search" && strings.TrimSpace(searchQ) == "" {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	store := newRedisStore(h.Redis)
	cacheKey := buildPornripsCatalogKey(catalogID, contentType, searchQ, genre, skip)
	if store != nil {
		if cached, err := store.getTorrentList(ctx, prefixPornripsCatalog+cacheKey); err == nil && len(cached) > 0 {
			torrents := dedupePornripsCatalog(cached)
			max := cfg.MaxResults
			if max <= 0 {
				max = 20
			}
			if len(torrents) > max {
				torrents = torrents[:max]
			}
			metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "")
			return CatalogResponse{Metas: metas}, err
		}
	}

	// Mongo-only: serve from the durable pornrips_entries store. A cold store
	// (0 docs, e.g. before the first PornripsSync tick) serves empty until the
	// background ingest walk populates it - no live WP/scrape fallback.
	torrents := dedupePornripsCatalog(h.fetchPornripsFromStore(ctx, cfg, catalogID, genre, searchQ, skip))
	max := cfg.MaxResults
	if max <= 0 {
		max = 20
	}
	if len(torrents) > max {
		torrents = torrents[:max]
	}
	if store != nil && len(torrents) > 0 {
		_ = store.setTorrentList(ctx, prefixPornripsCatalog+cacheKey, torrents, ttlCatalogList)
	}

	metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "")
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	return CatalogResponse{Metas: metas}, nil
}

// fetchPornripsFromStore serves a PornRips catalog from the durable Mongo store.
// Returns nil (caller falls through to live WP/scrape) when the store is cold or
// the handler has no store wired. genre is the curated option name; it is
// normalized (NormToken) before querying studio_norm/tags_norm so "Bang Bros" and
// "BangBros" match. "All"/empty genre on pr_studio/pr_tag behaves like pr_recent.
func (h *Handler) fetchPornripsFromStore(ctx context.Context, cfg Config, catalogID, genre, searchQ string, skip int) []catalogTorrent {
	if h.Pornrips == nil {
		return nil
	}
	max := cfg.MaxResults
	if max <= 0 {
		max = 20
	}
	g := strings.TrimSpace(genre)
	if g == "All" {
		g = ""
	}

	var entries []prmodels.PornripsEntry
	var err error
	switch catalogID {
	case "pr_recent":
		entries, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
	case "pr_search":
		q := strings.TrimSpace(searchQ)
		if q == "" {
			return nil
		}
		entries, err = h.Pornrips.SearchPornrips(ctx, q, skip, max)
	case "pr_studio":
		if g == "" {
			entries, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
		} else {
			entries, err = h.Pornrips.GetPornripsByStudio(ctx, prmodels.NormToken(g), skip, max)
		}
	case "pr_tag":
		if g == "" {
			entries, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
		} else {
			entries, err = h.Pornrips.GetPornripsByTag(ctx, prTagTokens(g), skip, max)
		}
	default:
		return nil
	}
	if err != nil || len(entries) == 0 {
		return nil
	}
	return entriesToCatalog(entries)
}

// entriesToCatalog maps durable pornrips_entries to catalog torrents. Poster
// prefers the TPDB/Stash-enriched poster, falling back to the WP featured image
// (wp_poster) so entries still render before the enrich sweep runs.
func entriesToCatalog(entries []prmodels.PornripsEntry) []catalogTorrent {
	out := make([]catalogTorrent, 0, len(entries))
	for _, e := range entries {
		if e.Slug == "" {
			continue
		}
		title := e.Title
		if title == "" {
			title = e.Slug
		}
		cover := e.Poster
		if cover == "" {
			cover = e.WpPoster
		}
		detail := e.DetailURL
		if detail == "" {
			detail = "https://pornrips.to/" + e.Slug + "/"
		}
		out = append(out, catalogTorrent{
			Title:      title,
			DetailURL:  detail,
			CoverImage: cover,
			Website:    "pornrips",
			Indexer:    "pornrips",
			// Carry the resolved infoHash/torrentURL so enriched entries' jstrm IDs
			// emit h:<infoHash> and stream opens skip the live detail-page fetch.
			InfoHash:   e.InfoHash,
			TorrentURL: e.TorrentURL,
		})
	}
	return out
}

func strMetaVal(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.Itoa(int(s))
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.Itoa(int(s))
	case json.Number:
		return s.String()
	}
	return ""
}

func buildPornripsCatalogKey(catalogID, contentType, searchQ, genre string, skip int) string {
	return catalogID + "|" + contentType + "|" + searchQ + "|" + genre + "|" + itoa(skip)
}

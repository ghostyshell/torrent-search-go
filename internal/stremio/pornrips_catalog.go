package stremio

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	prmodels "torrent-search-go/pkg/models"
)

type pornripsParams struct {
	Website  string
	Category string
	Query    string
}

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
			max := cfg.MaxResults
			if max <= 0 {
				max = 20
			}
			torrents := cached
			if len(torrents) > max {
				torrents = torrents[:max]
			}
			metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "", false)
			return CatalogResponse{Metas: metas}, err
		}
	}

	// Mongo-only: serve from the durable pornrips_entries store. A cold store
	// (0 docs, e.g. before the first PornripsSync tick) serves empty until the
	// background ingest walk populates it - no live WP/scrape fallback. Grouping
	// (multi-resolution rips of one scene -> one jstrg: entry) is done in Mongo by
	// findPornripsGroups, not post-fetch, so pagination operates on scenes.
	torrents := h.fetchPornripsFromStore(ctx, cfg, catalogID, genre, searchQ, skip)
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

	metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "", false)
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
// Grouping (multi-resolution rips of one scene -> one row) is done in Mongo by
// findPornripsGroups; groupsToCatalog then maps each group to one catalogTorrent.
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

	var groups []prmodels.PornripsGroup
	var err error
	switch catalogID {
	case "pr_recent":
		groups, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
	case "pr_search":
		q := strings.TrimSpace(searchQ)
		if q == "" {
			return nil
		}
		groups, err = h.Pornrips.SearchPornrips(ctx, q, skip, max)
	case "pr_studio":
		if g == "" {
			groups, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
		} else {
			groups, err = h.Pornrips.GetPornripsByStudio(ctx, prmodels.NormToken(g), skip, max)
		}
	case "pr_tag":
		if g == "" {
			groups, err = h.Pornrips.GetPornripsRecent(ctx, skip, max)
		} else {
			groups, err = h.Pornrips.GetPornripsByTag(ctx, prTagTokens(g), skip, max)
		}
	default:
		return nil
	}
	if err != nil || len(groups) == 0 {
		return nil
	}
	return groupsToCatalog(groups)
}

// groupsToCatalog maps durable pornrips_entries scene groups (from the Mongo
// aggregation) to catalog torrents. Poster prefers the TPDB/Stash-enriched
// poster, falling back to the WP featured image (wp_poster) so entries still
// render before the enrich sweep runs. A single-member group emits a plain
// catalogTorrent (Members nil) -> buildMetas emits a jstrm: id. A multi-member
// group (the 720p/1080p/4K rips of one scene) sets Members to every variant ->
// buildMetas emits a jstrg: group id so the stream route returns one stream per
// variant. The representative (highest-resolution member, members[0]) supplies
// the row's Title/CoverImage/DetailURL/InfoHash/TorrentURL.
func groupsToCatalog(groups []prmodels.PornripsGroup) []catalogTorrent {
	out := make([]catalogTorrent, 0, len(groups))
	for _, gr := range groups {
		e := gr.Representative
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
		row := catalogTorrent{
			Title:      title,
			DetailURL:  detail,
			CoverImage: cover,
			Website:    "pornrips",
			Indexer:    "pornrips",
			// Carry the representative's resolved infoHash/torrentURL so its jstrm
			// ID emits h:<infoHash> and stream opens skip the live detail-page fetch.
			InfoHash:   e.InfoHash,
			TorrentURL: e.TorrentURL,
		}
		if len(gr.Members) > 1 {
			members := make([]TorrentRecord, 0, len(gr.Members))
			for _, m := range gr.Members {
				mDetail := m.DetailURL
				if mDetail == "" {
					mDetail = "https://pornrips.to/" + m.Slug + "/"
				}
				mTitle := m.Title
				if mTitle == "" {
					mTitle = m.Slug
				}
				members = append(members, TorrentRecord{
					Title:      mTitle,
					InfoHash:   m.InfoHash,
					TorrentURL: m.TorrentURL,
					DetailURL:  mDetail,
					Website:    "pornrips",
					Indexer:    "pornrips",
				})
			}
			row.Members = members
		}
		out = append(out, row)
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

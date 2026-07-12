package stremio

import "strings"

// CatalogDef is one Stremio catalog entry before manifest extras are applied.
type CatalogDef struct {
	ID   string
	Name string
	Type string
	Base string
}

type qualityVariant struct {
	marker   string
	label    string
	category string
}

type sortVariant struct {
	suffix string
	label  string
}

type logicalCatalog struct {
	base        string
	name        string
	query       string
	orientation string
	qualities   []qualityVariant
}

var qualitiesDefault = []qualityVariant{
	{marker: "", label: "4K", category: "507"},
	{marker: "fhd", label: "1080p", category: "505"},
}

var sortVariants = []sortVariant{
	{suffix: "top", label: "Top"},
	{suffix: "recent", label: "Recent"},
}

var logicalCatalogs = []logicalCatalog{
	{base: "xxx", name: "XXX", query: ""},
	{base: "xxx_trans", name: "Trans", query: "trans", orientation: "trans"},
}

// StudioPresets is maintained in sync with the backend's studio search terms.
var StudioPresets = []string{
	"Vixen", "DorcelClub", "Blacked", "BrazzersExxtra", "Tushy", "WowGirls",
	"Milfy", "EvilAngel", "OnlyFans", "TushyRaw", "XVideosRED", "Private",
	"Nubiles", "SexMex", "Wifey", "MetArtX", "OnlyTarts", "PlayboyPlus",
	"PornWorld", "InterracialPass", "SexArt", "PornMegaLoad", "TabooHeat",
	"NubileFilms", "MetArt", "DeepLush", "Watch4Beauty", "ClubSweethearts",
	"AssParade", "BlacksOnBlondes", "JaysPOV", "BBCSurprise", "ILovePOV",
	"ALSScan", "BigTitCreampie", "TheLifeErotic", "Lubed", "DigitalPlayground",
	"ATKGirlfriends", "Bang Rammed", "MyFriendsHotMom", "Anilos",
	"TransRoommates", "Swallowed", "GenderX", "PureMature", "SexyCuckold",
	"MariskaX", "MeanBitches", "HussiePass", "PrimalFetish",
	"FamilyTherapyXXX", "MissaX", "ExploitedCollegeGirls", "PrivateSociety",
	"WoodmanCastingX", "MomComesFirst", "SisLovesMe", "SisSwap", "DaughterSwap",
	"PureTaboo", "Deeper", "ExxxtraSmall", "BackroomCastingCouch", "LegalPorno",
	"BlackedRaw", "PervMom", "TouchMyWife", "Milfty", "FreeUseFantasy",
	"FamilyStrokes", "PropertySex", "SketchySex", "TimTales", "ChaosMen",
	"Men.com", "Sean Cody", "Helix Studios", "Falcon Studios",
	"Girlsway", "MommysGirl", "GirlfriendsFilms", "AllGirlMassage",
	"WebYoung", "Lesbea", "Sweetheart Video",
	"Moodyz", "S1 No.1 Style", "Idea Pocket", "Prestige", "Attackers",
	"Wanz Factory", "FALENO", "SOD Create", "Madonna",
	"Caribbeancom", "1Pondo", "Heyzo", "Tokyo Hot", "10musume",
	"Pacopacomama", "Muramura", "FC2",
	"TransAngels", "TransSensual", "Transfixed", "Trans500", "GroobyGirls",
	"TGirls", "Ladyboy", "TSPlayground", "TransErotica", "TSRaw", "TS Factor",
	// Gay-male studios (HD 505, all 1080p-only) - verified on thehiddenbay.com.
	"BoyFun", "CockyBoys", "Staxus", "Latin Boyz", "Hardkinks", "Why Not Bi",
	// VR studios - VR is high-res; most have both 4K (507) and 1080p (505).
	// NaughtyAmericaVR has no 4K results (1080p-only).
	"VRBangers", "BadoinkVR", "VirtualRealPorn", "SexLikeReal", "CzechVR",
	"WankzVR", "NaughtyAmericaVR", "VRHush", "DarkRoomVR", "MilfVR",
	"RealJamVR", "VRAllure",
	// Ebony studios (Black-women brands). Only BrownBunnies has 4K results;
	// the rest are 1080p-only. Interracial brands are NOT ebony (excluded).
	"RoundAndBrown", "BrownBunnies", "GhettoGaggers", "We Fuck Black Girls",
	"Gloryhole Initiations", "WatchingMyMomGoBlack", "Cumbang",
	"BlackValleyGirls", "Evasive Angles", "West Coast Productions",
	"Black Ice", "Pinkyxxx",
}

var studio1080pOnly = map[string]struct{}{
	"FamilyTherapyXXX": {}, "PrivateSociety": {}, "MomComesFirst": {}, "PropertySex": {},
	"SketchySex": {}, "TimTales": {}, "ChaosMen": {},
	"Men.com": {}, "Sean Cody": {}, "Helix Studios": {}, "Falcon Studios": {},
	"WebYoung": {}, "Lesbea": {}, "Sweetheart Video": {},
	"Moodyz": {}, "S1 No.1 Style": {}, "Idea Pocket": {}, "Prestige": {}, "Attackers": {},
	"Wanz Factory": {}, "FALENO": {}, "SOD Create": {}, "Madonna": {},
	"Caribbeancom": {}, "1Pondo": {}, "Heyzo": {}, "Tokyo Hot": {}, "10musume": {},
	"Pacopacomama": {}, "Muramura": {}, "FC2": {},
	"TransAngels": {}, "TransSensual": {}, "TSPlayground": {}, "TransErotica": {}, "TSRaw": {}, "TS Factor": {},
	// Gay studios added - none have 4K results on HiddenBay
	"BoyFun": {}, "CockyBoys": {}, "Staxus": {}, "Latin Boyz": {}, "Hardkinks": {}, "Why Not Bi": {},
	// VR - NaughtyAmericaVR has no 4K results on HiddenBay (rest have both)
	"NaughtyAmericaVR": {},
	// Ebony - only BrownBunnies has 4K results on HiddenBay
	"RoundAndBrown": {}, "GhettoGaggers": {}, "We Fuck Black Girls": {},
	"Gloryhole Initiations": {}, "WatchingMyMomGoBlack": {}, "Cumbang": {},
	"BlackValleyGirls": {}, "Evasive Angles": {}, "West Coast Productions": {},
	"Black Ice": {}, "Pinkyxxx": {},
}

func studioSafeID(studio string) string {
	s := strings.ToLower(studio)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func studioOrientation(studio string) string {
	switch studio {
	case "SketchySex", "TimTales", "ChaosMen", "Men.com", "Sean Cody", "Helix Studios", "Falcon Studios",
		"BoyFun", "CockyBoys", "Staxus", "Latin Boyz", "Hardkinks", "Why Not Bi":
		return "gay"
	case "ALSScan", "WowGirls", "TheLifeErotic", "Girlsway", "MommysGirl", "GirlfriendsFilms",
		"AllGirlMassage", "WebYoung", "Lesbea", "Sweetheart Video":
		return "lesbian"
	case "TransRoommates", "GenderX", "TransAngels", "TransSensual", "Transfixed", "Trans500",
		"GroobyGirls", "TGirls", "Ladyboy", "TSPlayground", "TransErotica", "TSRaw", "TS Factor":
		return "trans"
	case "VRBangers", "BadoinkVR", "VirtualRealPorn", "SexLikeReal", "CzechVR",
		"WankzVR", "NaughtyAmericaVR", "VRHush", "DarkRoomVR", "MilfVR",
		"RealJamVR", "VRAllure":
		return "vr"
	case "RoundAndBrown", "BrownBunnies", "GhettoGaggers", "We Fuck Black Girls",
		"Gloryhole Initiations", "WatchingMyMomGoBlack", "Cumbang",
		"BlackValleyGirls", "Evasive Angles", "West Coast Productions",
		"Black Ice", "Pinkyxxx":
		return "ebony"
	case "Moodyz", "S1 No.1 Style", "Idea Pocket", "Prestige", "Attackers", "Wanz Factory",
		"FALENO", "SOD Create", "Madonna":
		return "jav_censored"
	case "Caribbeancom", "1Pondo", "Heyzo", "Tokyo Hot", "10musume", "Pacopacomama", "Muramura", "FC2":
		return "jav_uncensored"
	default:
		return "straight"
	}
}

func expandLogicalCatalogs(extraStudios []string) []logicalCatalog {
	list := append([]logicalCatalog(nil), logicalCatalogs...)
	seen := make(map[string]struct{})
	for _, studio := range append(append([]string(nil), StudioPresets...), extraStudios...) {
		if studio == "" {
			continue
		}
		slug := studioSafeID(studio)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		entry := logicalCatalog{
			base:        "xxx_studio_" + slug,
			name:        studio,
			query:       studio,
			orientation: studioOrientation(studio),
		}
		if _, only1080 := studio1080pOnly[studio]; only1080 {
			entry.qualities = []qualityVariant{{marker: "fhd", label: "1080p", category: "505"}}
		}
		list = append(list, entry)
	}
	return list
}

// GetAdultCatalogs returns the full list of adult catalog definitions (quality × sort per logical catalog).
func GetAdultCatalogs(extraStudios []string) []CatalogDef {
	catalogs := make([]CatalogDef, 0, 256)
	for _, c := range expandLogicalCatalogs(extraStudios) {
		quals := c.qualities
		if len(quals) == 0 {
			quals = qualitiesDefault
		}
		for _, q := range quals {
			qBase := c.base
			if q.marker != "" {
				qBase = c.base + "_" + q.marker
			}
			for _, v := range sortVariants {
				catalogs = append(catalogs, CatalogDef{
					ID:   qBase + "_" + v.suffix,
					Name: c.name + " " + q.label + " - " + v.label,
					Type: "Porn",
					Base: qBase,
				})
			}
		}
	}
	return catalogs
}

// isMainXxxBrowseCatalog reports whether base is the main XXX browse catalog
// (4K or 1080p). These are browse-only in the manifest; global search is handled
// by the dedicated Search catalog.
func isMainXxxBrowseCatalog(base string) bool {
	return base == "xxx" || base == "xxx_fhd"
}

// isMainAdultCatalogBase reports whether base is a main XXX/Trans browse catalog
// (the non-studio TPB logical catalogs). These sort with the non-studio board
// block rather than the TPB-Studio block.
func isMainAdultCatalogBase(base string) bool {
	return isMainXxxBrowseCatalog(base) || base == "xxx_trans" || base == "xxx_trans_fhd"
}

const PornSearchCatalogID = "search"

// isPornSearchCatalogID reports the global Porn search catalog. jav_search is
// the legacy id kept for existing installs.
func isPornSearchCatalogID(id string) bool {
	return id == PornSearchCatalogID || id == "jav_search"
}

// CompactStudioCatalogs returns one browse-only catalog per selected studio for
// compact mode: id is the bare `xxx_studio_{slug}` (no quality/sort suffix) and
// the name is just the studio. A studio is "selected" when any of its quality
// bases is enabled (or, with no allow-list, not disabled). 1080p-only studios
// only have the `_fhd` base, so only that base is consulted for them.
func CompactStudioCatalogs(extraStudios []string, enabled, disabled map[string]struct{}) []CatalogDef {
	out := make([]CatalogDef, 0)
	for _, c := range expandLogicalCatalogs(extraStudios) {
		if !strings.HasPrefix(c.base, "xxx_studio_") {
			continue
		}
		base4k := c.base
		baseFhd := c.base + "_fhd"
		_, only1080 := studio1080pOnly[c.name]

		selected := false
		switch {
		case enabled != nil:
			if _, ok := enabled[baseFhd]; ok {
				selected = true
			}
			if !selected && !only1080 {
				if _, ok := enabled[base4k]; ok {
					selected = true
				}
			}
		case only1080:
			if _, off := disabled[baseFhd]; !off {
				selected = true
			}
		default:
			if _, off := disabled[base4k]; !off {
				selected = true
			}
			if !selected {
				if _, off := disabled[baseFhd]; !off {
					selected = true
				}
			}
		}
		if !selected {
			continue
		}
		out = append(out, CatalogDef{
			ID:   c.base,
			Name: c.name,
			Type: "Porn",
			Base: c.base,
		})
	}
	return out
}

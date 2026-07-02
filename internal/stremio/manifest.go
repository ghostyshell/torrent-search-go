package stremio

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"

	appconfig "torrent-search-go/internal/config"
)

const (
	addonID      = "com.stremio.tpbporn"
	addonVersion = "1.9.77"
	addonName    = "TPB 4K Porn"
	manifestDescription = "4K & 1080p adult torrent catalogs from HiddenBay, PornRips, and Sukebei, plus HentaiMama episodes and Stripchat live HLS cams (240p-1080p). JAV catalog and ThePornDB scene browser with optional TPDB/StashDB metadata. Streams via 12 debrid providers (Real-Debrid, TorBox, and more) with P2P fallback and optional MediaFlow proxy. Works in Stremio and Nuvio (debrid only); toggle sources, compact studio catalogs, and save encrypted cross-device presets."
)

// StremioAddonsConfig is the ownership signature from stremio-addons.net.
var StremioAddonsConfig = map[string]string{
	"issuer":    "https://stremio-addons.net",
	"signature": "eyJhbGciOiJkaXIiLCJlbmMiOiJBMTI4Q0JDLUhTMjU2In0..IQX5eEtvLRfKwQeJCUNDgw.Hp_PbODSKBLLPdfRAI1mKTvl4lcFBIuKz7c8ZjaEOYmGRBadY05mOomG1pzzIRvGvmYtmZ4cuAsf959RZf-RCxfJF7Ce85VUkb0lobfkXX1jNAxQH8jgIJKnK4-bQcM2.A7FLyPyrK73lAbg1YD8QGw",
}

type externalGenres struct {
	HentaiGenres  []string `json:"HENTAI_GENRES"`
	HentaiTime    []string `json:"HENTAI_TIME"`
	HentaiStudios []string `json:"HENTAI_STUDIOS"`
	HentaiYears   []string `json:"HENTAI_YEARS"`
}

var externalGenreData externalGenres

const externalGenresJSON = `{"HENTAI_GENRES":["3D","Action","Adventure","Ahegao","Anal","Animal Girls","BDSM","Big Ass","Big Boobs","Blackmail","Blowjob","Bondage","Brainwashed","Bukkake","Cat Girl","Censored","Cheating","Comedy","Condom","Cosplay","Creampie","Cunnilingus","Cute & Funny","Dark Skin","Deepthroat","Demons","Doctor","Domination","Double Penetration","Drama","Dubbed","Ecchi","Elf","Eroge","Facesitting","Facial","Fantasy","Female Doctor","Female Teacher","Femdom","Filmed","Footjob","Furry","Futanari","Gangbang","Glasses","Group Sex","Gyaru","Handjob","Harem","HD","Historical","Horny Slut","Horror","Housewife","Humiliation","Idol","Incest","Inflation","Internal Cumshot","Lactation","Loli","Magical Girls","Maid","Martial Arts","Masturbation","Megane","MILF","Mind Break","Mind Control","Molestation","Monster","Monster Girl","Nekomimi","Non-japanese","NTR","Nuns","Nurse","Office Ladies","Oral Sex","Orc","Orgy","Paizuri","Plot","Police","POV","Pregnant","Princess","Prostitution","Public Sex","Rape","Reverse Rape","Rimjob","Romance","Scat","Schoolgirl","Sci-fi","Shimapan","Short","Shota","Slave","Small Breasts","Softcore","Sports","Squirting","Step Daughter","Step Mother","Step Sister","Stocking","Strap-on","Succubus","Super Power","Supernatural","Swimsuit","Teacher","Tentacles","Threesome","Toys","Train Molestation","Trap","Tsundere","Twin Tail","Ugly Bastard","Uncensored","Urination","Vampire","Vanilla","Virgin","Watersports","X-ray","Yaoi","Yuri"],"HENTAI_TIME":["This Week","This Month","3 Months","This Year"],"HENTAI_STUDIOS":["@G-NoeL","@OZ","01-Torte","0verflow","18th picture-story showhouse","2244/white","26RegionSFM","3D Anime Main Shop","3D Works","3dcg","3dmovie","69Girls","8bit","Aanix","Abnormal Junky","Abujan","Adult Source Media","Affect3D","AIC","Aim-ZERO","ainos","Akata","Alibi+","Alice Soft","Almond Collective","almondcollective","alons_factory","Amelialtie","Ammolite","Amour","Amusteven","Anifactory","Anik","Animac","AniMan","Animate","Anime Antenna Group","Anime Antenna Iinkai","anipolylife","Antechinus","anzuworks","Aokumashii","Apatite","Appetite","applemint","APPP","Ararza","Arms","Artcg3d","artifact","artman","At-2","Atelier Kaguya","Atelier KOB","Atelier Strawberry Pancakes","Bad Company","BEAM Entertainment","Bishop","Black Train","BlackBox","blue arrow garden","Blue Beard","Blue Eyes","BOMB! CUTE! BOMB!","BOOTLEG","bp","BraBusterSystem","BreakBottle","Bunnywalker","capsule soft","Caryo","Central Park Media","charm point","ChiChinoya","Chippai","Chocolat","Circle Cat","Cloud-9-Gate","Collaboration Works","Comet","Comic Media","Courreges Ace","Cranberry","Crimson","Crocore","Curenade","D-lis","D3","Daiei","Daisy","Dancing Queen","demodemon","denchu","dendendou","depression","Digital Graffiti","Digital Works","Discovery","Distortion","dodoro3D","Doll House","Dollhouse","Double Soft Cream","Doujin Fetish 2022","Doujin3aries","Ducat Inc.","Dynamic Planning","Ebimaru-do","EDGE","EDGE systems","EGAKIYA Kiyoshi","Erogos","Erotan Seijin","Etching Edge","evee","excess m","F.a.s","Final Booster","Final Fuck 7","firstpain","Five Ways","Flavors Soft","FreespaceP","Friends Media Station","Front Line","Frontier Works","Fruit","Futa","G Drain","Gold Bear","gomasen","gotonatural","Grand†cross","Green Bunny","Groovin Girls","GT-Four","Guilty+","gusya","Hamburg Gakari","Hentaibros","Himajin Planning","Hokiboshi","Honey Select","Hoods Entertainment","Hot Bear","HydraFXX","Hykobo","Illusion","Imokenpi","IMP","Innocent Grey","Ironbell","Ivory Tower","J.C.Staff","Jam","JapanAnime","Jellyfish","kate_sai","KENZsoft","Kinako no Yama","King Bee","Kitty Media","KN’s 3D Room","Knockout","Kyouki no Sybylla","Labo","Lanzfh","Lemon Heart","Lilith","loiter Manpuku3D","Love Guru Guru","LunaGazer","Lune Pictures","Madoromi Andon","MaFantia MaF","Magic Bus","Magin Label","Majin","Majin petit","Mary Jane","Media Bank","Media Blasters","MediaBank","megaromania","merienda","miconisomi","Milky","Milky Animation Label","MiMiA Cute","Misakura Nankotsu/Harthnir","Mousou Senka","MS Pictures","Nekoman","Nighthawk","Nihikime no Dozeu","None","None Found","Not Found","Nu Tech Digital","Nur"],"HENTAI_YEARS":["2025","2024","2023","2022","2021","2020","2019","2018","2017","2016","2015","2014","2013","2012","2011","2010","2009","2008","2007","2006","2005","2004","2003","2002","2001","2000","1999","1998","1997","1996","1995","1994","1993","1992","1991","1990","1989","1988","1987","1986","1984","1983","1981","1970","1969"]}`

func init() {
	_ = json.Unmarshal([]byte(externalGenresJSON), &externalGenreData)
}

// pornripsStudios is the curated pr_studio option list. Parent network brands
// (FakeHub, Full Porn Network, Team Skeet, Kink, Blowpass) were removed
// 2026-06-30: their studio_norm matched <10 streamable entries while their
// sub-site brands (Fake Taxi, FakeHub Originals, Sis Loves Me, Device Bondage,
// Whipped Ass, Throated, Swallowed, ...) remain and carry the content.
var pornripsStudios = []string{"Adult Prime", "Adult Time", "Anal Vids", "Aziani", "Filthy Kings", "It's POV", "MetArt", "Naughty America", "Nubiles", "Top Web Models", "1111Customs", "5K Porn", "Abby Winters", "All Girl Massage", "All Over 30", "ALS Angels", "ALS Scan", "Anal Mom", "Anal Only", "Anilos", "Ass Parade", "Asshole Fever", "ATK Exotics", "ATK Galleria", "ATK Girlfriends", "ATK Hairy", "Aunt Judys", "Aunt Judys XXX", "Backdoor POV", "Backroom Casting Couch", "Bang Bus", "BANG!", "BangBros 18", "BBC Pie", "BBC Surprise", "Beauty and the Senior", "Beauty Angels", "Big Gulp Girls", "Big Tit Cream Pie", "Big Tits Round Asses", "BJ Raw", "Blacked", "Blacked Raw", "Blacks On Blondes", "Brand New Amateurs", "Brat Tamer", "Bratty MILF", "Bratty Sis", "Brazzers Exxtra", "Breeding Material", "Broken Sluts", "Casting Couch X", "Casting Couch-HD", "Club Sweethearts", "Club Tug", "Creampie Angels", "Cuck Hunter", "Cuckold Sessions", "Cum Perfection", "Czech Sex Casting", "CzechBoobs", "Dad Crush", "Dane Jones", "Daughter JOI", "Daughter Swap", "Debt 4K", "Deep Lush", "Deeper", "Deepthroat Sirens", "Device Bondage", "Devil's Film", "DFXtra Originals", "Digital Playground", "Dirty Auditions", "Dirty Wives Club", "Dorcel Club", "Dungeon Sex", "Elegant Raw", "Erotica X", "Eternal Desire", "Everything Butt", "Evil Angel", "Exotic 4K", "Exxxtra Small", "Fake Taxi", "FakeHub Originals", "Family Strokes", "FemJoy", "Filthy Taboo", "Fitness Rooms", "Freak Mob Media", "Freeuse Fantasy", "Frolic Me", "FTV Girls", "FTV Milfs", "Fuck Studies", "Gangbang Creampie", "Gender X", "GirlCum", "Girls Out West", "Girlsway", "Gloryhole Secrets", "Good Morning Sex", "Got Filled", "Got Mylf", "GrandMams", "GrandParentsX", "Hard X", "Hardwerk", "Hegre", "Hijab Hookup", "Hijab Mylfs", "Hollandsche Passie", "HomeGrownEurope", "Hookup Hotshot", "Hot MILFs Fuck", "How Women Orgasm", "Hunt 4K", "Hussie Pass", "ILovePOV", "Immoral Live", "Inserted", "Interracial Vision", "IntimatePOV", "JapanHDV", "Jay's POV", "Jesse Loads Monster Facials", "JOI Babes", "Lady Lyne", "Lady Voyeurs", "Lara's Playground", "Legal Porno", "Lesbian Summer", "Let's Post It", "Love Her Feet", "Lubed", "ManyVids", "Mature 4K", "Mature NL", "MetArtX", "MILF AF", "MILFY", "Mom Drips", "Mom is Horny", "Mom Wants Creampie", "Mom Wants To Breed", "Mommy's Boy", "Mommy's Girl", "Moms Teach Sex", "Monsters Of Cock", "More POV", "Mr Lucky POV", "Mucha Sexo", "My Dirty Maid", "My Family Pies", "My First Sex Teacher", "My Friend's Hot Mom", "My Life In Miami", "MYLF Seeker", "Naughty Athletics", "Naughty Office", "Nebraska Coeds", "Net Girl", "New Sensations", "NF Busty", "Nookies Originals", "Nubile Films", "Nympho", "OfficePOV", "Only Teen Blowjobs", "OnlyTarts", "Oops Family", "Pascals Sub Sluts", "Passion HD", "Perfect 18", "Perv Mom", "Perv Therapy", "Petite POV", "Petite18", "Playboy Plus", "PlumperPass", "Porn Dude Casting", "Porn Fidelity", "Porn Force", "Porn Mega Load", "Porn World", "PornPlus", "Pornstar Wife", "POV Masters", "POV Perv", "POVD", "primemature", "Private", "Private Society", "Pure CFNM", "Pure Taboo", "PurgatoryX", "Pussy Patrol", "Reality Junkies", "Ricky's Room", "RK Prime", "S3xus", "Salsa XXX", "See Him Fuck", "Sex and Submission", "SexArt", "SexMex", "Shady Spa", "Shame 4K", "She Seduced Me", "She's Breeding Material", "Shoplyfter", "ShowerX", "Sin DeLuxe", "Sinful XXX", "Sis Loves Me", "Sis Swap", "Spank Monster", "Step Siblings", "Step Siblings Caught", "Strap Lez", "Swallowed", "Sweet FemDom", "Sweet Sinner", "Sweetheart Video", "Taboo Heat", "Teen From Bohemia", "Thai Girls Wild", "The Life Erotic", "The White Boxxx", "Throated", "Thundercock", "Tiny 4K", "Tonight's Girlfriend", "Touch My Wife", "Transfixed", "Tushy", "Tushy Raw", "Vicky at Home", "VIPissy", "Viv Thomas", "Vixen", "Watch 4 Beauty", "Wet and Pissy", "Wet and Puffy", "Whipped Ass", "Wifey", "Wifey's World", "Will Tile XXX", "Wow Girls"}

// PornripsStudioOptions is the exported form of the curated pornripsStudios list
// (the pr_studio genre options) for read-only tooling (cmd/tpbaudit) so it can
// audit the manifest's exact studio set without duplicating the list.
func PornripsStudioOptions() []string { return pornripsStudios }

type prCatalogDef struct {
	id           string
	name         string
	genre        bool
	options      []string
	search       bool
	hideFromHome bool
}

var pornripsCatalogDefs = []prCatalogDef{
	{id: "pr_recent", name: "PornRips · Recent"},
	{id: "pr_studio", name: "PornRips · Studio", genre: true, options: pornripsStudios, hideFromHome: true},
	{id: "pr_tag", name: "PornRips · Tag", genre: true, hideFromHome: true}, // options from prTagOptions(cfg)
	{id: "pr_search", name: "PornRips · Search", search: true, hideFromHome: true},
	// ThePornDB catalogs appear alongside PornRips in Discover.
	// PornRips catalogs list only PRT torrent releases from PornRips.to.
	// TPDB catalogs index scenes from all major studios via ThePornDB.
	{id: "tpdb_new", name: "ThePornDB · Recent (all studios)"},
	{id: "tpdb_search", name: "ThePornDB · Search", search: true, hideFromHome: true},
}

type externalCatalogDef struct {
	id           string
	name         string
	options      []string
	search       bool
	hideFromHome bool
}

// hentaiCatalogDefs declares the six Hentai catalogs. Go owns the manifest now;
// the Node edge proxies it verbatim (no edge-side catalog list to keep in sync).
func hentaiCatalogDefs() []externalCatalogDef {
	return []externalCatalogDef{
		{id: "hentai_new", name: "Hentai · New", options: externalGenreData.HentaiTime},
		{id: "hentai_top", name: "Hentai · Top Rated", options: externalGenreData.HentaiGenres},
		{id: "hentai_all", name: "Hentai · All", options: externalGenreData.HentaiGenres},
		{id: "hentai_studios", name: "Hentai · Studios", options: externalGenreData.HentaiStudios, hideFromHome: true},
		{id: "hentai_years", name: "Hentai · Year", options: externalGenreData.HentaiYears, hideFromHome: true},
		{id: "hentai_search", name: "Hentai · Search", search: true, hideFromHome: true},
	}
}

// stripchatCatalogDefs returns the four live-cam Stripchat catalogs. Each is a
// browseable live-model row that is also searchable by username (search optional,
// so the row stays visible on Home). type is Porn; ids are listed in catalog.go.
func stripchatCatalogDefs() []externalCatalogDef {
	return []externalCatalogDef{
		{id: "sc_girls", name: "Stripchat · Girls", search: true},
		{id: "sc_couples", name: "Stripchat · Couples", search: true},
		{id: "sc_guys", name: "Stripchat · Guys", search: true},
		{id: "sc_trans", name: "Stripchat · Trans", search: true},
	}
}

var postfixSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

// sortCatalogsByName sorts manifest catalog entries alphabetically by display
// name (case-insensitive, stable). Mutates the slice in place. Used to order the
// home/discover board into two sorted blocks: non-TPB-Studio catalogs first,
// then TPB-Studio catalogs.
func sortCatalogsByName(catalogs []map[string]interface{}) {
	sort.SliceStable(catalogs, func(i, j int) bool {
		ni, _ := catalogs[i]["name"].(string)
		nj, _ := catalogs[j]["name"].(string)
		return strings.ToLower(ni) < strings.ToLower(nj)
	})
}

// BuildManifest returns the complete Stremio addon manifest.
// extraStudios are merged with built-in studio presets (from KV addon:xxx_studios).
func BuildManifest(cfg Config, baseURL string, env *appconfig.Config, extraStudios []string, prTagOpts []string) map[string]interface{} {
	disabled := make(map[string]struct{}, len(cfg.DisabledCatalogs))
	for _, id := range cfg.DisabledCatalogs {
		disabled[id] = struct{}{}
	}

	var enabled map[string]struct{}
	if len(cfg.EnabledCatalogs) > 0 {
		enabled = make(map[string]struct{}, len(cfg.EnabledCatalogs))
		for _, id := range cfg.EnabledCatalogs {
			enabled[id] = struct{}{}
		}
	}

	homeGenre := []map[string]interface{}{}
	if cfg.HideFromHome {
		homeGenre = []map[string]interface{}{
			{"name": "genre", "isRequired": true, "options": []string{"All"}},
		}
	}
	opt := map[string]interface{}{"isRequired": false}

	sources := cfg.Sources
	if len(sources) == 0 {
		sources = []string{"piratebay"}
	}

	var tpbCatalogs []map[string]interface{}
	// XXX and Trans are TPB-sourced but not studios; they sort with the
	// non-studio block (see assembly below), so collect them separately.
	var tpbMainCatalogs []map[string]interface{}
	if containsSource(sources, "piratebay") {
		enabledSorts := make(map[string]struct{}, len(cfg.EnabledSorts))
		for _, s := range cfg.EnabledSorts {
			enabledSorts[s] = struct{}{}
		}
		for _, section := range GetAdultCatalogs(extraStudios) {
			if cfg.CompactStudios && strings.Contains(section.Base, "_studio_") {
				continue // studios emitted as one compact catalog below
			}
			if cfg.CompactStudios && isMainXxxBrowseCatalog(section.Base) {
				continue // main XXX emitted as one compact catalog below
			}
			if cfg.CompactStudios && (section.Base == "xxx_trans" || section.Base == "xxx_trans_fhd") {
				continue // Trans emitted as one compact catalog below
			}
			if enabled != nil {
				if _, ok := enabled[section.Base]; !ok {
					continue
				}
			} else if _, off := disabled[section.Base]; off {
				continue
			}
			if _, ok := enabledSorts[catalogSortSuffix(section.ID)]; !ok {
				continue
			}

			extra := []map[string]interface{}{}
			switch {
			case strings.Contains(section.Base, "_studio_"), isMainXxxBrowseCatalog(section.Base):
				// Browse-only: studio rows and main XXX catalogs skip global search.
				extra = append(extra, mergeExtra(map[string]interface{}{"name": "skip"}, opt))
			default:
				extra = append(extra,
					mergeExtra(map[string]interface{}{"name": "search"}, opt),
					mergeExtra(map[string]interface{}{"name": "skip"}, opt),
				)
			}
			extra = append(extra, homeGenre...)

			entry := map[string]interface{}{
				"type":  section.Type,
				"id":    section.ID,
				"name":  section.Name,
				"extra": extra,
			}
			if isMainAdultCatalogBase(section.Base) {
				tpbMainCatalogs = append(tpbMainCatalogs, entry)
			} else {
				tpbCatalogs = append(tpbCatalogs, entry)
			}
		}

		// Compact mode: one browse-only catalog per selected studio, named just
		// the studio, plus bare `xxx` and `xxx_trans` for the main XXX and Trans
		// catalogs. Results are merged across each catalog's enabled quality bases
		// and sorts at serve time (see serveCompactMergedCatalog). Gated on
		// enabledSorts to mirror the non-compact path (no sorts = none emitted).
		if cfg.CompactStudios && len(enabledSorts) > 0 {
			compact := CompactStudioCatalogs(extraStudios, enabled, disabled)
			if baseEnabled(cfg, "xxx") || baseEnabled(cfg, "xxx_fhd") {
				compact = append(compact, CatalogDef{ID: "xxx", Name: "XXX", Type: "Porn", Base: "xxx"})
			}
			if baseEnabled(cfg, "xxx_trans") || baseEnabled(cfg, "xxx_trans_fhd") {
				compact = append(compact, CatalogDef{ID: "xxx_trans", Name: "Trans", Type: "Porn", Base: "xxx_trans"})
			}
			for _, c := range compact {
				extra := []map[string]interface{}{mergeExtra(map[string]interface{}{"name": "skip"}, opt)}
				extra = append(extra, homeGenre...)
				entry := map[string]interface{}{
					"type":  c.Type,
					"id":    c.ID,
					"name":  c.Name,
					"extra": extra,
				}
				if isMainAdultCatalogBase(c.Base) {
					tpbMainCatalogs = append(tpbMainCatalogs, entry)
				} else {
					tpbCatalogs = append(tpbCatalogs, entry)
				}
			}
		}

		// Dedicated performer/code/title search catalog. search is required so it
		// is search-only (no generic browse row on the board); a query is resolved
		// via StashDB performer lookup into product codes (plus the raw title search).
		// Only emit on part 1 (or unsplit) so a multi-part install does not duplicate
		// the Search row across every part - Search resolves across all piratebay
		// content regardless of chunk, so one instance (part 1) is sufficient.
		if cfg.Group <= 1 {
			tpbCatalogs = append(tpbCatalogs, map[string]interface{}{
				"type": "Porn",
				"id":   PornSearchCatalogID,
				"name": "Search",
				"extra": []map[string]interface{}{
					{"name": "search", "isRequired": true},
					mergeExtra(map[string]interface{}{"name": "skip"}, opt),
				},
			})
		}
	}

	var pornripsCatalogs []map[string]interface{}
	if containsSource(sources, "pornrips") {
		// pr_tag is TPDB/StashDB-enriched (the enrich sweep fills entry tags from
		// scene tags), so gate it on a server metadata key like tpdb_cat/stashdb_cat.
		// pr_studio stays visible: it is a curated list and the ingest job backs it
		// even before enrichment runs.
		prTagEnabled := tpdbServerActive(cfg, env) || stashdbServerActive(env)
		// pr_tag options come from the curated TPDB/StashDB category taxonomy
		// (prTagOptions). The dynamic store-derived top-tags builder was removed:
		// it ran an un-indexable $unwind/$group-by-count COLLSCAN on every manifest
		// fetch and drove Mongo CPU to 100% under manifest re-fetch traffic.
		tagOpts := prTagOpts
		if len(tagOpts) == 0 {
			tagOpts = prTagOptions(cfg)
		}
		pornripsCatalogs = pornripsManifestCatalogs(disabled, homeGenre, cfg.DisabledPrStudios, tagOpts, prTagEnabled)
	}

	var hentaiCatalogs []map[string]interface{}
	if containsSource(sources, "hentai") {
		hentaiCatalogs = externalManifestCatalogs(hentaiCatalogDefs(), disabled, homeGenre)
	}

	var stripchatCatalogs []map[string]interface{}
	if containsSource(sources, "stripchat") {
		stripchatCatalogs = externalManifestCatalogs(stripchatCatalogDefs(), disabled, homeGenre)
	}

	stashdbActive := stashdbConfigured(cfg, env)

	var sukebeiCatalogs []map[string]interface{}
	if containsSource(sources, "sukebei") && stashdbActive {
		sukebeiCatalogs = sukebeiManifestCatalogs(disabled, homeGenre, cfg.EnabledSorts)
	}

	var categoryCatalogs []map[string]interface{}
	if tpdbServerActive(cfg, env) && len(cfg.TpdbCategories) > 0 {
		categoryCatalogs = append(categoryCatalogs, map[string]interface{}{
			"type": "Porn",
			"id":   tpdbCatalogID,
			"name": "TPDB",
			"extra": []map[string]interface{}{
				{"name": "genre", "isRequired": true, "options": append([]string{"All"}, categoryNames(cfg.TpdbCategories)...)},
				{"name": "skip"},
			},
		})
	}
	if stashdbServerActive(env) && len(cfg.StashdbCategories) > 0 {
		categoryCatalogs = append(categoryCatalogs, map[string]interface{}{
			"type": "Porn",
			"id":   stashdbCatalogID,
			"name": "theStashDB",
			"extra": []map[string]interface{}{
				{"name": "genre", "isRequired": true, "options": append([]string{"All"}, categoryNames(cfg.StashdbCategories)...)},
				{"name": "skip"},
			},
		})
	}

	// Board order: non-TPB-Studio catalogs (PornRips, Hentai, Sukebei,
	// Stripchat, TPDB/StashDB) plus the TPB main XXX/Trans catalogs, sorted
	// alphabetically by name, then the TPB-Studio (piratebay) catalogs sorted
	// alphabetically. The two groups fall out of the per-source slices above;
	// sorting each block in place.
	nonStudio := make([]map[string]interface{}, 0,
		len(pornripsCatalogs)+len(hentaiCatalogs)+len(sukebeiCatalogs)+len(stripchatCatalogs)+len(categoryCatalogs)+len(tpbMainCatalogs))
	nonStudio = append(nonStudio, pornripsCatalogs...)
	nonStudio = append(nonStudio, hentaiCatalogs...)
	nonStudio = append(nonStudio, sukebeiCatalogs...)
	nonStudio = append(nonStudio, stripchatCatalogs...)
	nonStudio = append(nonStudio, categoryCatalogs...)
	nonStudio = append(nonStudio, tpbMainCatalogs...)
	sortCatalogsByName(nonStudio)
	sortCatalogsByName(tpbCatalogs)

	catalogs := make([]map[string]interface{}, 0, len(nonStudio)+len(tpbCatalogs))
	catalogs = append(catalogs, nonStudio...)
	catalogs = append(catalogs, tpbCatalogs...)

	prov := DetectProvider(cfg)
	isCatalogSplit := cfg.GroupTotal > 1 && cfg.Group > 0

	postfix := strings.TrimSpace(cfg.NamePostfix)
	postfixSlug := slugifyPostfix(postfix)

	id := addonID
	if prov != nil {
		id += "." + prov.Token
	}
	if isCatalogSplit {
		id += ".g" + itoa(cfg.Group)
	}
	if postfixSlug != "" {
		id += "." + postfixSlug
	}

	name := addonName
	if prov != nil {
		name += " - " + prov.Label
	}
	if isCatalogSplit {
		name += " (" + itoa(cfg.Group) + "/" + itoa(cfg.GroupTotal) + ")"
	}
	if postfix != "" {
		name += " " + postfix
	}

	// addonVersion is the baseline; ADDON_VERSION env overrides it so deploys
	// can keep the backend manifest current without a code change. The Node
	// edge also stamps the live addon version onto the proxied manifest, so
	// Stremio sees the real version regardless; this covers direct-backend hits.
	version := addonVersion
	if v := strings.TrimSpace(os.Getenv("ADDON_VERSION")); v != "" {
		version = v
	}

	manifest := map[string]interface{}{
		"id":                  id,
		"version":             version,
		"name":                name,
		"stremioAddonsConfig": StremioAddonsConfig,
		"description":         manifestDescription,
		"resources":           []string{"catalog", "meta", "stream"},
		"types":               []string{"Porn", "hentai", "series"},
		// idPrefixes: Go owns the manifest; the Node edge proxies it verbatim.
		// hs:/hmm- are Hentai (Go self-scrapes HentaiMama hmm-; legacy hs:
		// resolves to no streams, deprecated). htv- was dropped (HentaiTV
		// removed - its r2 CDN 403s from the backend and it was never part of
		// the configured source). hse- was dropped (HentaiSea excluded).
		"idPrefixes":          []string{"jstrm:", "jstrg:", "porndb:", "stash:", "hs:", "hmm-", "sc:"},
		"catalogs":            catalogs,
		"behaviorHints": map[string]interface{}{
			"adult":        true,
			"p2p":          true,
			"configurable": true,
		},
	}
	if baseURL != "" {
		manifest["logo"] = strings.TrimSuffix(baseURL, "/") + "/icon.svg"
		if hints, ok := manifest["behaviorHints"].(map[string]interface{}); ok {
			hints["configureUrl"] = strings.TrimSuffix(baseURL, "/") + "/configure"
		}
	}
	return manifest
}

// catalogSortSuffix returns the trailing sort variant of a TPB adult catalog id
// ("recent" or "top"). All TPB adult ids end with one of these two suffixes.
func catalogSortSuffix(catalogID string) string {
	if strings.HasSuffix(catalogID, "_recent") {
		return "recent"
	}
	if strings.HasSuffix(catalogID, "_top") {
		return "top"
	}
	return ""
}

func mergeExtra(base, defaults map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range base {
		out[k] = v
	}
	return out
}

// applyHomeGenre marks an existing genre extra required when hideFromHome is on.
// Appending a second genre extra leaves the first optional and Stremio still
// loads the catalog row on Home.
func applyHomeGenre(extra []map[string]interface{}, homeGenre []map[string]interface{}) []map[string]interface{} {
	if len(homeGenre) == 0 {
		return extra
	}
	for _, e := range extra {
		if name, _ := e["name"].(string); name == "genre" {
			e["isRequired"] = true
			return extra
		}
	}
	return append(extra, homeGenre...)
}

func containsSource(sources []string, want string) bool {
	for _, s := range sources {
		if s == want {
			return true
		}
	}
	return false
}

// stashdbConfigured reports whether StashDB metadata can be resolved for this
// install - either from the user's config key or a server-side env key.
func stashdbConfigured(cfg Config, env *appconfig.Config) bool {
	if strings.TrimSpace(cfg.StashdbKey) != "" {
		return true
	}
	return env != nil && env.Metadata.StashDBAPIKey != ""
}

// tpdbServerActive reports whether the server env has a TPDB key for the shared
// category warmer cache.
func tpdbServerActive(_ Config, env *appconfig.Config) bool {
	return env != nil && env.Metadata.TPDBAPIKey != ""
}

// stashdbServerActive reports whether the server env has a StashDB key for the
// shared category warmer cache.
func stashdbServerActive(env *appconfig.Config) bool {
	return env != nil && env.Metadata.StashDBAPIKey != ""
}

func pornripsManifestCatalogs(disabled map[string]struct{}, homeGenre []map[string]interface{}, disabledPrStudios []string, tagOptions []string, prTagEnabled bool) []map[string]interface{} {
	disabledStudio := make(map[string]struct{}, len(disabledPrStudios))
	for _, s := range disabledPrStudios {
		disabledStudio[s] = struct{}{}
	}

	out := make([]map[string]interface{}, 0, len(pornripsCatalogDefs))
	for _, c := range pornripsCatalogDefs {
		if disabled != nil {
			if _, off := disabled[c.id]; off {
				continue
			}
		}
		// pr_tag depends on TPDB/StashDB enrichment; hide it when no server key
		// is configured (parity with tpdb_cat/stashdb_cat).
		if c.id == "pr_tag" && !prTagEnabled {
			continue
		}
		extra := make([]map[string]interface{}, 0, 4)
		if c.search {
			searchExtra := map[string]interface{}{"name": "search"}
			if c.hideFromHome {
				searchExtra["isRequired"] = true
			}
			extra = append(extra, searchExtra)
		}
		if c.genre {
			options := c.options
			if c.id == "pr_studio" {
				filtered := make([]string, 0, len(options))
				for _, s := range options {
					if _, off := disabledStudio[s]; off {
						continue
					}
					filtered = append(filtered, s)
				}
				options = filtered
			} else if c.id == "pr_tag" {
				// Reuse the TPDB/StashDB category taxonomy so pr_tag options line up
				// with the tags the enrich sweep writes (from TPDB/Stash scene tags).
				options = tagOptions
			}
			genreExtra := map[string]interface{}{
				"name":    "genre",
				"options": append([]string{"All"}, options...),
			}
			if c.hideFromHome {
				genreExtra["isRequired"] = true
			}
			extra = append(extra, genreExtra)
		}
		skipExtra := map[string]interface{}{"name": "skip"}
		if len(homeGenre) > 0 {
			skipExtra["isRequired"] = false
		}
		extra = append(extra, skipExtra)
		if !c.hideFromHome {
			extra = applyHomeGenre(extra, homeGenre)
		}
		out = append(out, map[string]interface{}{
			"type":  "Porn",
			"id":    c.id,
			"name":  c.name,
			"extra": extra,
		})
	}
	return out
}

func externalManifestCatalogs(defs []externalCatalogDef, disabled map[string]struct{}, homeGenre []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(defs))
	for _, c := range defs {
		if disabled != nil {
			if _, off := disabled[c.id]; off {
				continue
			}
		}
		extra := make([]map[string]interface{}, 0, 4)
		if c.search {
			searchExtra := map[string]interface{}{"name": "search"}
			if c.hideFromHome {
				searchExtra["isRequired"] = true
			}
			extra = append(extra, searchExtra)
		}
		if len(c.options) > 0 {
			genreExtra := map[string]interface{}{
				"name":    "genre",
				"options": append([]string{"All"}, c.options...),
			}
			if c.hideFromHome {
				genreExtra["isRequired"] = true
			}
			extra = append(extra, genreExtra)
		}
		skipExtra := map[string]interface{}{"name": "skip"}
		if len(homeGenre) > 0 {
			skipExtra["isRequired"] = false
		}
		extra = append(extra, skipExtra)
		if !c.hideFromHome {
			extra = applyHomeGenre(extra, homeGenre)
		}
		// Hentai catalogs are exposed under Stremio's native "hentai" type so the
		// client renders them as a series source (episode picker + streams per video).
		catType := "Porn"
		if strings.HasPrefix(c.id, "hentai_") {
			catType = "hentai"
		}
		out = append(out, map[string]interface{}{
			"type":  catType,
			"id":    c.id,
			"name":  c.name,
			"extra": extra,
		})
	}
	return out
}

func slugifyPostfix(postfix string) string {
	s := strings.ToLower(postfix)
	s = postfixSlugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 20 {
		s = s[:20]
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(digits[i:])
	if neg {
		return "-" + s
	}
	return s
}

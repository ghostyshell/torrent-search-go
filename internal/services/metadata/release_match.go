package metadata

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StudioAliases maps compact torrent studio tokens to canonical site names.
var StudioAliases = map[string]string{
	"sexmex":           "Sex Mex",
	"mommysgirl":       "Mommy's Girl",
	"bigtitcreampie":   "Big Tit Creampie",
	"myfriendshotmom":  "My Friends Hot Mom",
	"blacksonblondes":  "Blacks On Blondes",
	"girlfriendsfilms": "Girlfriends Films",
	"allgirlmassage":   "All Girl Massage",
	"bangrammed":       "Bang Rammed",
	"bangyngr":         "YNGR",
	"bangbros18":       "Bang Bros 18",
	"evilangel":        "Evil Angel",
	"alsscan":          "ALS Scan",
	"joibabes":         "JOI Babes",
	"purgatoryx":       "Purgatory X",
	"deepthroatsirens": "Deep Throat Sirens",
	"dickhddaily":      "Dick HD Daily",
	"nubiles":          "Nubiles",
	"clubsweethearts":  "Club Sweethearts",
	"pornmegaload":     "PornMegaLoad",
	"atkgirlfriends":   "ATK Girlfriends",
	"tabooheat":        "Taboo Heat",
}

var (
	tailRE   = regexp.MustCompile(`(?i)\b(xxx|2160p|1080p|720p|480p|4k|uhd|sd|web[-\s]?dl|web[-\s]?rip|bd[-\s]?rip|hd[-\s]?rip|hdtv|remux|bluray|hevc|x\s?26[45]|h\s?26[45]|av1|xvid|divx|mp4|mkv|wmv|avi|m4v|aac|flac|ddp?5|remastered|multisub|imageset|siterip|kvcd|vr180|vr|rq)\b`)
	onlyFansDashRE = regexp.MustCompile(`(?i)^OnlyFans\s*-\s*(.+?)\s*-\s*(.+)$`)
	withCoStarRE   = regexp.MustCompile(`(?i)\b(?:with|and)\s+([A-Za-z][A-Za-z0-9]*(?:\s+[A-Za-z][A-Za-z0-9]*)?)\s*$`)
	byCoStarRE     = regexp.MustCompile(`(?i)\bby\s+([A-Za-z][A-Za-z0-9]*(?:\s+[A-Za-z][A-Za-z0-9]*)?)\s*$`)
	dateRE   = regexp.MustCompile(`\b(20\d{2}|\d{2})[ ._-](\d{2})[ ._-](\d{2})\b`)
	nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
)

// JAV releases are keyed by a product code rather than a studio-name title.
// These match a single leading token (brackets/dots already split off).
var (
	javBracketRE = regexp.MustCompile(`[\[\]()]`)
	javFC2RE     = regexp.MustCompile(`(?i)^FC2-?PPV-?(\d{3,8})$`)
	javLabeledRE  = regexp.MustCompile(`^([A-Za-z]{2,7})-?(\d{2,6})$`)
	javSuffixedRE = regexp.MustCompile(`(?i)^([A-Za-z]{2,7})-(\d{2,6})(?:[-_][A-Za-z0-9]{1,8})$`)
	javDateRE     = regexp.MustCompile(`^(\d{6})[-_](\d{1,4})$`)
	javDigitsRE  = regexp.MustCompile(`\d+`)
	// Some uncensored sites label scenes as "<SITE> <number>" with the label and
	// number in separate tokens (Heyzo: "HEYZO 3877"). A whitelist keeps this from
	// matching descriptive "<word> <number>" tokens like "Vol 119" or "BOUGA 143".
	javSiteRE = regexp.MustCompile(`(?i)\b(HEYZO)[ _-]+(\d{3,5})\b`)
)

// ParsedRelease holds structured parts of a torrent release name.
type ParsedRelease struct {
	Raw        string
	Studio     string
	Performer  string
	Date       string
	Scene      string
	Code       string
	Tokens     []string
	CleanQuery string
}

// MatchCandidate is a scene used for release verification.
type MatchCandidate struct {
	Title      string
	Studio     string
	Performers []string
	Date       string
	Code       string
}

// ParseRelease splits a raw torrent/release name into structured parts.
func ParseRelease(rawName string) ParsedRelease {
	raw := rawName
	s := raw
	s = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`).ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = regexp.MustCompile(`\s{2,}`).ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	if m := onlyFansDashRE.FindStringSubmatch(s); m != nil {
		performer := strings.TrimSpace(m[1])
		scene := cleanSceneTitle(m[2])
		return ParsedRelease{
			Raw:        raw,
			Studio:     "OnlyFans",
			Performer:  performer,
			Scene:      scene,
			Code:       parseJAVCode(rawName),
			Tokens:     Tokenize(performer + " " + scene),
			CleanQuery: strings.TrimSpace("OnlyFans " + PrimaryPerformer(performer) + " " + scene),
		}
	}

	var date string
	dateStart := -1
	dateEnd := -1
	if dm := dateRE.FindStringSubmatchIndex(s); dm != nil {
		yy := s[dm[2]:dm[3]]
		if len(yy) == 2 {
			yy = "20" + yy
		}
		mo := s[dm[4]:dm[5]]
		d := s[dm[6]:dm[7]]
		moN, _ := strconv.Atoi(mo)
		dN, _ := strconv.Atoi(d)
		if moN >= 1 && moN <= 12 && dN >= 1 && dN <= 31 {
			date = yy + "-" + mo + "-" + d
			dateStart = dm[0]
			dateEnd = dm[1]
		}
	}

	var studio, scene string
	if date != "" && dateStart >= 0 {
		studio = strings.TrimSpace(s[:dateStart])
		scene = strings.TrimSpace(s[dateEnd:])
	} else {
		parts := strings.Fields(s)
		if len(parts) > 0 {
			studio = parts[0]
		}
		if len(parts) > 1 {
			scene = strings.Join(parts[1:], " ")
		}
	}

	scene = cleanSceneTitle(scene)

	cleanQuery := strings.TrimSpace(strings.TrimSpace(studio + " " + scene))
	return ParsedRelease{
		Raw:        raw,
		Studio:     studio,
		Date:       date,
		Scene:      scene,
		Code:       parseJAVCode(rawName),
		Tokens:     Tokenize(scene),
		CleanQuery: cleanQuery,
	}
}

func cleanSceneTitle(scene string) string {
	if loc := tailRE.FindStringIndex(scene); loc != nil {
		scene = scene[:loc[0]]
	}
	scene = regexp.MustCompile(`-\s*\w+\s*$`).ReplaceAllString(scene, " ")
	scene = regexp.MustCompile(`\b\d{4,}\b`).ReplaceAllString(scene, " ")
	scene = regexp.MustCompile(`\s{2,}`).ReplaceAllString(scene, " ")
	return strings.TrimSpace(scene)
}

// PrimaryPerformer returns the first billed name from an OnlyFans performer field.
func PrimaryPerformer(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(strings.ToLower(s), " aka "); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// OnlyFansCoStarProbes returns performer+co-star search strings for TPDB/StashDB.
func OnlyFansCoStarProbes(performer, scene string) []string {
	perf := PrimaryPerformer(performer)
	if perf == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	for _, re := range []*regexp.Regexp{byCoStarRE, withCoStarRE} {
		if m := re.FindStringSubmatch(scene); m != nil {
			add(perf + " " + strings.TrimSpace(m[1]))
		}
	}
	return out
}

// parseJAVCode extracts a JAV product code from a release name, returning ""
// when none is present. The separator and case are canonicalized (SSIS001 →
// SSIS-001) but zero-padding is preserved so the result re-parses to itself;
// CodesMatch handles padding differences. It is therefore safe to call on a
// previously extracted code.
//
// The code can sit anywhere in the title - many releases lead with an English
// description and place the code near the end before a studio tag (e.g.
// "Double J-cup ... MIDA-590 (MOODYZ)"), so the whole title is scanned. Real DVD
// codes carry a separator (MIDA-590); the last separated match wins (it sits
// closest to the studio tag), with an un-separated match (SSIS001) as fallback.
// The ≥2-letter + ≥2-digit shape excludes codec/quality tokens (X264, DDP5).
func parseJAVCode(raw string) string {
	cleaned := javBracketRE.ReplaceAllString(raw, " ")
	cleaned = strings.NewReplacer(".", " ", ":", " ", ";", " ", ",", " ").Replace(cleaned)
	if m := javSiteRE.FindStringSubmatch(cleaned); m != nil {
		return strings.ToUpper(m[1]) + "-" + m[2]
	}
	var separated, plain string
	for _, tok := range strings.Fields(cleaned) {
		if m := javFC2RE.FindStringSubmatch(tok); m != nil {
			return "FC2-PPV-" + m[1]
		}
		if m := javDateRE.FindStringSubmatch(tok); m != nil {
			return m[1] + "-" + m[2]
		}
		code, separatedHit := javCodeFromToken(tok)
		if code != "" {
			if separatedHit {
				separated = code
			} else if plain == "" {
				plain = code
			}
		}
	}
	if separated != "" {
		return separated
	}
	return plain
}

func javCodeFromToken(tok string) (code string, separated bool) {
	if m := javLabeledRE.FindStringSubmatch(tok); m != nil {
		return strings.ToUpper(m[1]) + "-" + m[2], strings.ContainsAny(tok, "-_")
	}
	if m := javSuffixedRE.FindStringSubmatch(tok); m != nil {
		return strings.ToUpper(m[1]) + "-" + m[2], true
	}
	return "", false
}

// IsJAVCode reports whether s contains a recognizable JAV product code.
func IsJAVCode(s string) bool { return parseJAVCode(s) != "" }

// NormalizedJAVCode returns a canonical key for the same product code, or "".
func NormalizedJAVCode(code string) string { return normalizeJAVCode(code) }

// CodesMatch reports whether two codes are the same JAV product code, ignoring
// zero-padding (SSIS-001 == SSIS-00001). Returns false when either is not a code.
func CodesMatch(a, b string) bool {
	na := normalizeJAVCode(a)
	return na != "" && na == normalizeJAVCode(b)
}

// normalizeJAVCode canonicalizes a code and strips zero-padding from each numeric
// run so padding variants compare equal. Returns "" when input is not a JAV code.
func normalizeJAVCode(code string) string {
	c := parseJAVCode(code)
	if c == "" {
		return ""
	}
	return javDigitsRE.ReplaceAllStringFunc(c, func(s string) string {
		n, _ := strconv.Atoi(s)
		return strconv.Itoa(n)
	})
}

// Tokenize returns lowercase words of length >= 3.
func Tokenize(s string) []string {
	words := strings.Fields(nonAlnum.ReplaceAllString(strings.ToLower(s), " "))
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// ExpandStudioToken maps a compact torrent studio token to a canonical TPDB name.
func ExpandStudioToken(studio string) string {
	s := compact(studio)
	if s == "" {
		return ""
	}
	if alias, ok := StudioAliases[s]; ok {
		return alias
	}
	return strings.TrimSpace(studio)
}

func compact(s string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(s), "")
}

// StudiosMatch reports whether a parsed studio token and candidate studio name match.
func StudiosMatch(studioToken, candidateStudio string) bool {
	a := compact(studioToken)
	b := compact(candidateStudio)
	if a == "" || b == "" {
		return false
	}
	if a == b || strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	if alias, ok := StudioAliases[a]; ok {
		c := compact(alias)
		if c != "" && (c == b || strings.Contains(c, b) || strings.Contains(b, c)) {
			return true
		}
	}
	return false
}

// DatesClose reports whether two ISO dates are within maxDays of each other.
func DatesClose(a, b string, maxDays int) bool {
	if a == "" || b == "" {
		return false
	}
	da, errA := time.Parse("2006-01-02", a[:min(len(a), 10)])
	db, errB := time.Parse("2006-01-02", b[:min(len(b), 10)])
	if errA != nil || errB != nil {
		return false
	}
	diff := da.Sub(db)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(maxDays)*24*time.Hour
}

// PerformerOverlap checks performer/title token overlap with parsed scene text.
func PerformerOverlap(sceneTokens []string, candidate MatchCandidate) bool {
	set := make(map[string]struct{}, len(sceneTokens))
	for _, t := range sceneTokens {
		set[t] = struct{}{}
	}
	if len(set) == 0 {
		return false
	}

	names := make([]string, 0, len(candidate.Performers)+1)
	names = append(names, candidate.Performers...)
	if len(names) == 0 && candidate.Title != "" {
		names = append(names, candidate.Title)
	}
	for _, n := range names {
		for _, w := range Tokenize(n) {
			if _, ok := set[w]; ok {
				return true
			}
		}
	}
	return false
}

// VerifyMatch decides whether a candidate scene matches a parsed release.
func VerifyMatch(parsed ParsedRelease, candidate MatchCandidate) bool {
	sceneTokens := parsed.Tokens
	if len(sceneTokens) == 0 {
		sceneTokens = Tokenize(parsed.Scene)
	}
	studioOK := StudiosMatch(parsed.Studio, candidate.Studio)
	dateClose := DatesClose(parsed.Date, candidate.Date, 3)
	dateExact := DatesClose(parsed.Date, candidate.Date, 0)
	perfOK := PerformerOverlap(sceneTokens, candidate)

	// When the torrent carries a studio+date prefix, a studio match alone is not
	// enough - require the release date to line up (or an exact date + performer).
	if parsed.Date != "" {
		if studioOK && dateClose && (perfOK || IsGoodMatch(candidate.Title, parsed.Scene)) {
			return true
		}
		if perfOK && dateExact {
			return true
		}
		if studioOK && candidate.Studio != "" && !dateClose {
			return false
		}
		return IsGoodMatch(candidate.Title, parsed.Scene)
	}

	if studioOK && (dateClose || perfOK) {
		return true
	}
	if perfOK && dateExact {
		return true
	}
	matchTitle := parsed.Scene
	if parsed.Performer != "" {
		matchTitle = strings.TrimSpace(PrimaryPerformer(parsed.Performer) + " " + parsed.Scene)
		if !perfOK {
			return false
		}
	}
	return IsGoodMatch(candidate.Title, matchTitle)
}

// IsGoodMatch is the legacy title-token overlap heuristic.
func IsGoodMatch(candTitle, torrentTitle string) bool {
	if candTitle == "" || torrentTitle == "" {
		return false
	}
	norm := func(s string) string {
		return strings.TrimSpace(nonAlnum.ReplaceAllString(strings.ToLower(s), " "))
	}
	a := norm(candTitle)
	b := norm(torrentTitle)
	if a == b || strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}

	aWords := make(map[string]struct{})
	for _, w := range strings.Fields(a) {
		if len(w) >= 3 {
			aWords[w] = struct{}{}
		}
	}
	bWords := strings.Fields(b)
	sig := 0
	for _, w := range bWords {
		if len(w) >= 3 {
			sig++
		}
	}
	if sig == 0 {
		return false
	}
	hits := 0
	for _, w := range bWords {
		if len(w) >= 3 {
			if _, ok := aWords[w]; ok {
				hits++
			}
		}
	}
	return float64(hits)/float64(sig) >= 0.5
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

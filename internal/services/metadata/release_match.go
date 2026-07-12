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

// performerPairProbes builds q= keyword probes for multi-performer release
// names whose title is a run of co-performer aliases, with or without an
// "OnlyFans" prefix and with or without a date. Examples (all the same scene):
//
//	"OnlyFans 23 06 17 June Liu SpicyGum Juneliu Emiri Momota Mizukaw" (OnlyFans + date)
//	"June Liu SpicyGum Juneliu Emiri Momota Mizukawasumire BGG threesome mp4" (no prefix, no date)
//	"OnlyFans Emiri Momota aka Mizukawa Sumire June Liu aka JuneLiu SpicyGum BG ..." (aka separators)
//
// The canonical scene is indexed under one performer pair, but TPDB parse=
// misses the alias soup and q='s strict AND rejects the full token run, so we
// probe pair combinations: the first 2-token name as the primary paired with
// each later 2-token name window. One window lands on the canonical pair
// ("June Liu Emiri Momota"); VerifyMatch (dated) or VerifyPairDescriptor
// (no date) rejects the rest.
//
// Returns nil unless the title is a flat soup with no parsed explicit co-star
// (dash-form titles have their own OnlyFansCoStarProbes path). Dated non-OnlyFans
// titles resolve via the studio+date parse/q loop, so dated pair probes are
// OnlyFans-only; no-date soups are probed regardless of studio because extra
// indexers (bitsearch) often drop the "OnlyFans" prefix and the date.
// NoDatePairProbeTitle reports whether parsed is a no-date flat performer soup
// that SearchMetadataProbe resolves via the pair-probe path. The pair probes are
// derived from parsed (not the query string), so the lookup is variant-independent:
// looping query variants re-fires the same 6-probe fan-out per variant and only burns
// the caller's deadline and trips TPDB rate limits. Callers that loop query variants
// (loadTpdbMeta) use this to collapse to a single SearchMetadataProbe call.
func NoDatePairProbeTitle(parsed ParsedRelease) bool {
	return parsed.Date == "" && len(performerPairProbes(parsed)) > 0
}

func performerPairProbes(parsed ParsedRelease) []string {
	if parsed.Performer != "" {
		return nil
	}
	if parsed.Date != "" && !strings.EqualFold(parsed.Studio, "OnlyFans") {
		return nil
	}

	// Build the name-token run. OnlyFans sits in Studio and is not a performer,
	// so Scene already holds the names. For a no-date title whose first token
	// ParseRelease put in Studio (the first performer's first name, e.g. "June"
	// in "June Liu SpicyGum..."), re-prepend Studio so the 2-token grouping
	// starts at the real primary name.
	tokens := strings.Fields(parsed.Scene)
	if parsed.Studio != "" && !strings.EqualFold(parsed.Studio, "OnlyFans") {
		tokens = append([]string{parsed.Studio}, tokens...)
	}
	if len(tokens) < 4 {
		return nil
	}

	primary := strings.Join(tokens[:2], " ")
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	// ponytail: cap at 6 probes to bound TPDB q= call volume per title. Sliding
	// 2-token windows (not every-2) so "aka"-separated soups still land a probe
	// on the canonical pair; only windows where both tokens look like names are
	// kept, which drops "aka Mizukawa", "BGG threesome", etc.
	const maxProbes = 6
	for i := 2; i+1 < len(tokens) && len(out) < maxProbes; i++ {
		if !isNameToken(tokens[i]) || !isNameToken(tokens[i+1]) {
			continue
		}
		add(primary + " " + tokens[i] + " " + tokens[i+1])
	}
	return out
}

// isNameToken reports whether s looks like a performer name token: a capitalised
// alphabetic word whose first letter is upper and second lower. This excludes
// ALL-CAPS descriptors ("BGG", "BG", "POV"), lowercase descriptors ("threesome",
// "aka"), and connectors ("and", "with").
func isNameToken(s string) bool {
	if len(s) < 2 {
		return false
	}
	r := []rune(s)
	if r[0] < 'A' || r[0] > 'Z' {
		return false
	}
	if r[1] < 'a' || r[1] > 'z' {
		return false
	}
	for _, c := range r[2:] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// splitProbe splits a "primary partner" probe string back into its two names.
func splitProbe(probe string) (primary, partner string) {
	f := strings.Fields(probe)
	if len(f) >= 2 {
		primary = strings.Join(f[:2], " ")
	}
	if len(f) > 2 {
		partner = strings.Join(f[2:], " ")
	}
	return primary, partner
}

// descriptorStop lists scene tokens that are not useful disambiguators (they
// appear in most porn titles), so VerifyPairDescriptor skips them when looking
// for a descriptor overlap.
var descriptorStop = map[string]struct{}{
	"aka": {}, "and": {}, "with": {}, "by": {}, "for": {}, "the": {}, "of": {}, "her": {}, "his": {},
}

// VerifyPairDescriptor accepts a TPDB candidate for a no-date flat performer
// soup when (a) both probe performers match distinct candidate performers and
// (b) a scene descriptor token - any scene token that is not part of either
// probe performer's name and is not a stopword - also appears in the candidate
// title. The pair requirement is the real gate (a specific two-performer collab
// narrows TPDB to a small set of scenes); the descriptor is the disambiguator
// the missing date would otherwise provide, so a pair with several scenes does
// not silently pull the first one's cover.
func VerifyPairDescriptor(parsed ParsedRelease, primary, partner string, candidate MatchCandidate) bool {
	i1, ok1 := candidateHasPerformer(primary, candidate)
	i2, ok2 := candidateHasPerformer(partner, candidate)
	if !ok1 || !ok2 || i1 == i2 {
		return false
	}
	names := probeNameTokens(primary, partner)
	for _, w := range Tokenize(parsed.Scene) {
		if _, skip := names[w]; skip {
			continue
		}
		if _, skip := descriptorStop[w]; skip {
			continue
		}
		if titleHasToken(candidate.Title, w) {
			return true
		}
	}
	return false
}

// candidateHasPerformer reports whether any candidate performer's token set is
// a superset of the probe performer's tokens, returning its index. A probe name
// like "June Liu" ({june, liu}) matches a candidate "June Liu" but not "Liu Yue".
func candidateHasPerformer(perf string, candidate MatchCandidate) (int, bool) {
	pt := tokenSet(perf)
	if len(pt) == 0 {
		return -1, false
	}
	for i, cp := range candidate.Performers {
		if subsetTokens(pt, tokenSet(cp)) {
			return i, true
		}
	}
	return -1, false
}

func probeNameTokens(names ...string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, n := range names {
		for _, w := range Tokenize(n) {
			out[w] = struct{}{}
		}
	}
	return out
}

func tokenSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range Tokenize(s) {
		out[w] = struct{}{}
	}
	return out
}

func subsetTokens(a, b map[string]struct{}) bool {
	for t := range a {
		if _, ok := b[t]; !ok {
			return false
		}
	}
	return true
}

func titleHasToken(title, w string) bool {
	for _, tw := range Tokenize(title) {
		if tw == w {
			return true
		}
	}
	return false
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
		// The release date is the strongest discriminator a TPB scene title
		// carries (the "Studio YY MM DD" prefix). When the candidate also has a
		// date it must line up: a scene on a different date is a different scene
		// however much the title tokens overlap, so don't let the title
		// heuristic below pull an unrelated cover. PerformerOverlap falls back
		// to the candidate title, so without this gate a different-studio scene
		// sharing a couple of scene words is wrongly accepted.
		if candidate.Date != "" {
			return dateClose && IsGoodMatch(candidate.Title, parsed.Scene)
		}
		// Dateless candidate (e.g. a movie record): require the studio to agree
		// before trusting title-token overlap alone.
		return studioOK && IsGoodMatch(candidate.Title, parsed.Scene)
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

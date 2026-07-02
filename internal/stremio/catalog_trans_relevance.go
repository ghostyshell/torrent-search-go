package stremio

import (
	"regexp"
	"strings"
)

// transRejectRE drops obvious non-porn hits when optional indexers search the bare
// token "trans" (translation, transfer, political spam, etc.).
var transRejectRE = regexp.MustCompile(`(?i)(?:` +
	`translat(?:e|ed|ion|or|ing)|` +
	`transfer(?:red|ring)?|` +
	`transform(?:er|ers|ation)?|` +
	`trans(?:parent|parency|it|mission|mit|mitter|cript|cription|cribe|code|atlantic|ylvania|ient|ponder)|` +
	`light[\s._-]?novel|` +
	`officially[\s._-]?trans|` +
	`anti[\s._-]?trans|` +
	`lost[\s._-]?in[\s._-]?trans|` +
	`trans[\s._-]?rights|` +
	`trans[\s._-]?phob|` +
	`trans[\s._-]?hate` +
	`)`)

// transAcceptRE matches common adult trans / TS release naming.
var transAcceptRE = regexp.MustCompile(`(?i)(?:` +
	`shemale|ladyboy|tgirl|tgirls|` +
	`transsexual|transgender|` +
	`transangels|trans[\s._-]?angels|` +
	`trans500|trans[\s._-]?500|` +
	`transfixed|trans[\s._-]?fixed|` +
	`transsensual|trans[\s._-]?sensual|` +
	`transerotica|trans[\s._-]?erotica|` +
	`transactive|trans[\s._-]?active|` +
	`genderx|gender[\s._-]?x|` +
	`grooby(?:girls)?|` +
	`tsraw|ts[\s._-]?raw|` +
	`ts[\s._-]?factor|ts[\s._-]?playground|` +
	`transroommate|trans[\s._-]?roommate|` +
	`trans[\s._-]?pool|` +
	`trans[\s._-]?babe|trans[\s._-]?cutie|` +
	`trans[\s._-]?on[\s._-]?trans|` +
	`bareback[\s._-]?trans|` +
	`\bxxx\b.*\btrans\b|\btrans\b.*\bxxx\b` +
	`)`)

var transWordRE = regexp.MustCompile(`(?i)(?:^|[\s\[(/._-])trans(?:[\s\])/.:_-]|$)`)

func isTransCatalogID(catalogID string) bool {
	return strings.Contains(catalogID, "xxx_trans")
}

func isPrimaryAdultIndexer(website string) bool {
	switch website {
	case catalogScraper, "hiddenbay":
		return true
	default:
		return false
	}
}

// fanoutAdultSearchQuery widens the HiddenBay "trans" keyword for optional
// indexers toward adult naming that those engines understand better.
func fanoutAdultSearchQuery(query string) string {
	if strings.EqualFold(strings.TrimSpace(query), "trans") {
		return "shemale transgender transsexual"
	}
	return query
}

func normalizeTransTitle(title string) string {
	s := strings.ToLower(title)
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ", "/", " ", "\\", " ").Replace(s)
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

// matchesTransAdultTitle reports whether a torrent title plausibly belongs in the
// Trans porn catalog (not translation, politics, or general movies).
func matchesTransAdultTitle(title string) bool {
	norm := normalizeTransTitle(title)
	if norm == "" {
		return false
	}
	if transRejectRE.MatchString(norm) {
		return false
	}
	if transAcceptRE.MatchString(norm) {
		return true
	}
	return transWordRE.MatchString(norm)
}

func filterTransRelevance(torrents []catalogTorrent) []catalogTorrent {
	out := torrents[:0]
	for _, t := range torrents {
		src := t.Website
		if src == "" {
			src = t.Indexer
		}
		if isPrimaryAdultIndexer(src) || matchesTransAdultTitle(t.Title) {
			out = append(out, t)
		}
	}
	return out
}

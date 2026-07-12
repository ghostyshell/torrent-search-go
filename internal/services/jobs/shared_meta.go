package jobs

import (
	"regexp"
	"strconv"
	"strings"

	"torrent-search-go/pkg/models"
)

// MergeShared combines TPDB and StashDB shared metadata (field-level). It is the
// single merge implementation shared by the Stremio meta/catalog handlers and the
// background jobs, so a merged record is identical wherever it is produced.
func MergeShared(tpdb, stashdb *SharedMeta) *SharedMeta {
	if tpdb == nil && stashdb == nil {
		return nil
	}
	if tpdb != nil && stashdb == nil {
		return tpdb
	}
	if tpdb == nil && stashdb != nil {
		return stashdb
	}
	merged := &SharedMeta{
		Title:       pickShared(tpdb.Title, stashdb.Title, "tpdb-first"),
		Description: pickShared(tpdb.Description, stashdb.Description, "longer"),
		Poster:      pickShared(tpdb.Poster, stashdb.Poster, "image"),
		Background:  pickShared(tpdb.Background, stashdb.Background, "image"),
		Year:        pickShared(tpdb.Year, stashdb.Year, "year"),
		Cast:        mergeUniqueShared(tpdb.Cast, stashdb.Cast),
		Tags:        mergeUniqueShared(tpdb.Tags, stashdb.Tags),
		Genres:      mergeUniqueShared(tpdb.Genres, stashdb.Genres),
		Source:      mergeSource(tpdb.Source, stashdb.Source),
	}
	if merged.Title == "" {
		merged.Title = tpdb.Title
		if merged.Title == "" {
			merged.Title = stashdb.Title
		}
	}
	return merged
}

var sdbImageRE = regexp.MustCompile(`(?i)stashdb|cdn\.stash`)

func pickShared(a, b, strategy string) string {
	aN := strings.TrimSpace(a)
	bN := strings.TrimSpace(b)
	if aN == "" {
		return bN
	}
	if bN == "" {
		return aN
	}
	switch strategy {
	case "longer":
		if len(aN) >= len(bN) {
			return aN
		}
		return bN
	case "image":
		aIsSdb := sdbImageRE.MatchString(aN)
		bIsSdb := sdbImageRE.MatchString(bN)
		if aIsSdb && !bIsSdb {
			return aN
		}
		if bIsSdb && !aIsSdb {
			return bN
		}
		if len(aN) >= len(bN) {
			return aN
		}
		return bN
	case "year":
		ay, _ := strconv.Atoi(aN)
		by, _ := strconv.Atoi(bN)
		if ay >= by {
			return aN
		}
		return bN
	default:
		return aN
	}
}

func mergeUniqueShared(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := make(map[string]struct{})
	for _, arr := range [][]string{a, b} {
		for _, n := range arr {
			k := strings.TrimSpace(n)
			if k == "" {
				continue
			}
			low := strings.ToLower(k)
			if _, ok := seen[low]; ok {
				continue
			}
			seen[low] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// mergeSource records which sources contributed to a merged record.
func mergeSource(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a != "" && b != "" && a != b:
		return a + "+" + b
	case a != "":
		return a
	default:
		return b
	}
}

// sharedToPayload converts a jobs.SharedMeta into the durable Mongo payload.
func sharedToPayload(m *SharedMeta) models.SharedMetaPayload {
	if m == nil {
		return models.SharedMetaPayload{}
	}
	return models.SharedMetaPayload{
		Title:       m.Title,
		Description: m.Description,
		Poster:      m.Poster,
		Background:  m.Background,
		Year:        m.Year,
		Cast:        m.Cast,
		Tags:        m.Tags,
		Genres:      m.Genres,
		Source:      m.Source,
	}
}

// payloadToShared converts a durable Mongo payload back into a jobs.SharedMeta.
func payloadToShared(p *models.SharedMetaPayload) *SharedMeta {
	if p == nil {
		return nil
	}
	return &SharedMeta{
		Title:       p.Title,
		Description: p.Description,
		Poster:      p.Poster,
		Background:  p.Background,
		Year:        p.Year,
		Cast:        p.Cast,
		Tags:        p.Tags,
		Genres:      p.Genres,
		Source:      p.Source,
	}
}

package metadata

import (
	"context"
	"errors"
	"strings"
)

// MetadataTitlesForLookup returns title variants to probe TPDB/StashDB with.
// It is the single source of truth for which query variants a metadata lookup
// tries, shared by the Stremio catalog live path (stremio.loadTpdbMeta /
// loadStashMeta) and the background MetaEnricher (jobs.MetaEnricher) via
// SearchMetadataVariants, so the two enrichment paths cannot drift in which
// titles they probe.
func MetadataTitlesForLookup(title string) []string {
	seen := make(map[string]string)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = s
	}

	add(title)
	if strings.Contains(title, ".") {
		add(strings.ReplaceAll(title, ".", " "))
	}
	parsed := ParseRelease(title)
	add(parsed.CleanQuery)
	if parsed.Performer != "" && parsed.Scene != "" {
		perf := PrimaryPerformer(parsed.Performer)
		add(perf + " " + parsed.Scene)
		add(parsed.Studio + " " + perf + " " + parsed.Scene)
		for _, probe := range OnlyFansCoStarProbes(parsed.Performer, parsed.Scene) {
			add(probe)
		}
	}
	if parsed.Studio != "" && parsed.Scene != "" {
		add(parsed.Studio + " " + parsed.Scene)
		if expanded := ExpandStudioToken(parsed.Studio); expanded != parsed.Studio {
			add(expanded + " " + parsed.Scene)
		}
	}
	if strings.Contains(parsed.Studio, " ") {
		parts := strings.Fields(parsed.Studio)
		if tail := parts[len(parts)-1]; tail != "" && parsed.Scene != "" {
			add(tail + " " + parsed.Scene)
			if expanded := ExpandStudioToken(tail); expanded != tail {
				add(expanded + " " + parsed.Scene)
			}
		}
	}
	if parsed.Scene != "" {
		if parsed.Performer == "" {
			add(parsed.Scene)
		}
	}

	out := make([]string, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	return out
}

// SearchMetadataVariants runs SearchMetadataProbe over the MetadataTitlesForLookup
// variants for title and returns the first verified match. No-date performer
// soups collapse to a single probe (parsed-keyed) as in the catalog live path.
// Returns (meta, rateLimited): rateLimited is true if any probe hit a TPDB rate
// limit and no match was found, so callers can requeue instead of recording a
// confirmed miss.
//
// This is the single TPDB probe implementation shared by the Stremio catalog
// live path and the background MetaEnricher, replacing the former divergence
// where the enricher used SearchMetadata (one raw-title probe) and the catalog
// used a multi-variant probe loop.
func (c *TPDBClient) SearchMetadataVariants(ctx context.Context, title string) (*NormalizedMeta, bool) {
	parsed := ParseRelease(title)
	probes := MetadataTitlesForLookup(title)
	if NoDatePairProbeTitle(parsed) {
		probes = []string{title}
	}
	rateLimited := false
	for _, probe := range probes {
		meta, err := c.SearchMetadataProbe(ctx, parsed, probe)
		if err != nil {
			if errors.Is(err, ErrTPDBRateLimited) {
				rateLimited = true
			}
			continue
		}
		if meta != nil {
			return meta, false
		}
	}
	return nil, rateLimited
}

// SearchMetadataVariants runs SearchMetadataProbe over the MetadataTitlesForLookup
// variants for title (StashDB has no rate-limit error; it swallows graphql
// errors and returns nil, so the second return is always false, kept for
// signature symmetry with TPDBClient.SearchMetadataVariants).
//
// Single StashDB probe implementation shared by the Stremio catalog live path
// and the background MetaEnricher.
func (c *StashDBClient) SearchMetadataVariants(ctx context.Context, title, detailURL string) (*NormalizedMeta, bool) {
	parsed := ParseRelease(title)
	for _, probe := range MetadataTitlesForLookup(title) {
		meta, err := c.SearchMetadataProbe(ctx, parsed, probe, detailURL)
		if err != nil || meta == nil {
			continue
		}
		return meta, false
	}
	return nil, false
}
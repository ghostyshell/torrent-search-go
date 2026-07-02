package jobs

import "context"

// PornripsSync is the sole background job that discovers new PornRips entries
// and populates Mongo with their metadata + torrent links. One tick runs the two
// existing sweeps in order - ingest (pornrips.to WP feed -> UpsertPornripsEntry)
// then enrich (.torrent -> infoHash backfill via pornripsTorrentBackfill, plus
// TPDB/Stash scene metadata via enrichOnePornripsEntry) - and merges their result
// maps (namespaced ingest_*/enrich_*) so a single tick reports both. Reuses
// PornripsIngest and PornripsEnrich unchanged; no third job talks to pornrips.to /
// TPDB / StashDB.
func (r *Runner) PornripsSync(ctx context.Context) (map[string]interface{}, error) {
	ingest, err := r.PornripsIngest(ctx)
	enrich, enrichErr := r.PornripsEnrich(ctx)
	out := mergeSyncResults(ingest, enrich)
	if err != nil {
		return out, err
	}
	return out, enrichErr
}

// mergeSyncResults merges the ingest and enrich result maps into one self-
// describing map. "success" is set true; per-sweep counts are namespaced
// (ingest_upserted, enrich_scanned, enrich_torrentResolved, ...) so the only
// colliding key ("success") does not clobber a sweep's counts.
func mergeSyncResults(ingest, enrich map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"success": true}
	for k, v := range ingest {
		if k == "success" {
			continue
		}
		out["ingest_"+k] = v
	}
	for k, v := range enrich {
		if k == "success" {
			continue
		}
		out["enrich_"+k] = v
	}
	return out
}
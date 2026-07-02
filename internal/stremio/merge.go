package stremio

import (
	"torrent-search-go/internal/services/jobs"
)

// mergeMetadata combines TPDB and StashDB shared metadata (field-level). The
// implementation lives in the jobs package (jobs.MergeShared) so the Stremio
// serve path and the background jobs produce identical merged records.
func mergeMetadata(tpdb, stashdb *jobs.SharedMeta) *jobs.SharedMeta {
	return jobs.MergeShared(tpdb, stashdb)
}

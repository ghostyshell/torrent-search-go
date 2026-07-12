package jobs

// SukebeiCatalogEntry is a StashDB-resolved catalog row backed by a Sukebei torrent.
type SukebeiCatalogEntry struct {
	Meta    StremioMetaPreview `json:"meta"`
	Torrent CatalogTorrent     `json:"torrent"`
}

var sukebeiCatalogIDs = []struct {
	ID   string
	Sort string
}{
	{ID: "sukebei_top", Sort: "7"},
	{ID: "sukebei_recent", Sort: "3"},
}

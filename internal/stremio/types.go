package stremio

// MetaPreview is a Stremio catalog list entry.
type MetaPreview struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Poster      string `json:"poster,omitempty"`
	Background  string `json:"background,omitempty"`
	Description string `json:"description,omitempty"`
	ReleaseInfo string `json:"releaseInfo,omitempty"`
	PosterShape string `json:"posterShape,omitempty"`
}

// Meta is a full Stremio metadata object.
type Meta struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Poster      string   `json:"poster,omitempty"`
	Background  string   `json:"background,omitempty"`
	Description string   `json:"description,omitempty"`
	ReleaseInfo string   `json:"releaseInfo,omitempty"`
	PosterShape string   `json:"posterShape,omitempty"`
	Website     string   `json:"website,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Runtime     string   `json:"runtime,omitempty"`
	Links       []Link   `json:"links,omitempty"`
	Videos      []Video  `json:"videos,omitempty"`
}

// Link is a Stremio meta link entry. category is required: stremio-core
// deserializes Link with a non-optional category field, so omitting it fails the
// whole meta object ("No metadata was found") even on a 200 response.
type Link struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

// Video is a Stremio episode entry inside a series meta response.
type Video struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
	Released   string `json:"released,omitempty"`
	Thumbnail  string `json:"thumbnail,omitempty"`
	Overview   string `json:"overview,omitempty"`
	FirstAired string `json:"firstAired,omitempty"`
}

// CatalogResponse is returned by the catalog handler.
type CatalogResponse struct {
	Metas []MetaPreview `json:"metas"`
}

// MetaResponse is returned by the meta handler.
type MetaResponse struct {
	Meta *Meta `json:"meta"`
}

// TorrentRecord is stored in Redis under torrent:v1:{jstrmId}.
type TorrentRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	InfoHash   string `json:"infoHash"`
	MagnetLink string `json:"magnetLink"`
	TorrentURL string `json:"torrentUrl"`
	DetailURL  string `json:"detailUrl"`
	Website    string `json:"website"`
	Indexer    string `json:"indexer"`
	Size       string `json:"size"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	CoverImage string `json:"coverImage"`
	Quality    string `json:"quality,omitempty"` // catalog quality scope: "4k" | "fhd"
}

// itemPayload is the JSON embedded in jstrm: item IDs.
type itemPayload struct {
	H string `json:"h"`
	T string `json:"t"`
	U string `json:"u"`
	W string `json:"w"`
	D string `json:"d,omitempty"`
	Q string `json:"q,omitempty"` // catalog quality scope: "4k" | "fhd"
}

// catalogTorrent mirrors the normalized list shape in Redis cat:v1:* keys.
type catalogTorrent struct {
	Title      string `json:"title"`
	Size       string `json:"size"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	InfoHash   string `json:"infoHash"`
	MagnetLink string `json:"magnetLink"`
	TorrentURL string `json:"torrentUrl"`
	DetailURL  string `json:"detailUrl"`
	CoverImage string `json:"coverImage"`
	Website    string `json:"website"`
	Indexer    string `json:"indexer"`
	Quality    string `json:"quality"`
}

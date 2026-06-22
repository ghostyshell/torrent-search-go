package metadata

// NormalizedMeta is the shared metadata shape written to tpdb-shared / stashdb-shared.
type NormalizedMeta struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Poster      string   `json:"poster,omitempty"`
	Background  string   `json:"background,omitempty"`
	Year        string   `json:"year,omitempty"`
	Cast        []string `json:"cast,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Source      string   `json:"source,omitempty"`
}

package jobs

import (
	_ "embed"
	"encoding/json"
)

//go:embed studioSearchTerms.json
var studioSearchTermsJSON []byte

// StudioSearchTerms returns studio filter queries (Node parity).
func StudioSearchTerms() []string {
	var studios []string
	if err := json.Unmarshal(studioSearchTermsJSON, &studios); err != nil {
		return nil
	}
	return studios
}

package magnetio

import (
	"strings"
)

// mochItemMeta returns a minimal Stremio meta object for debrid-prefixed ids
// such as rd:hash, pm:id, tb:id, dl:id, pu:id.
func mochItemMeta(cfg Config, contentType, id string) map[string]interface{} {
	if id == "" {
		return nil
	}
	idx := strings.Index(id, ":")
	if idx < 0 {
		return nil
	}
	mochID := id[:idx]
	itemID := id[idx+1:]
	if itemID == "" {
		return nil
	}

	client := mochClientByID(mochID)
	if client == nil {
		return nil
	}
	apiKey := apiKeyForMoch(cfg, mochID)
	if len(apiKey) < minMochKeyLen {
		return nil
	}

	name := serviceNameForMoch(mochID)
	return map[string]interface{}{
		"id":   id,
		"type": contentType,
		"name": name + " item " + itemID,
	}
}

func serviceNameForMoch(mochID string) string {
	for _, m := range MochOptions {
		if m.ID == mochID {
			return m.Name
		}
	}
	return strings.ToUpper(mochID)
}

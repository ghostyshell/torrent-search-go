package stremio

import "testing"

func TestSharedMetaMissIDScopesByAPIKey(t *testing.T) {
	metaID := "abc123"
	server := sharedMetaMissID(metaID, "server-key")
	user := sharedMetaMissID(metaID, "user-key")
	if server == user {
		t.Fatal("different API keys should not share a miss sentinel")
	}
	if sharedMetaMissID(metaID, "server-key") != server {
		t.Fatal("miss id should be stable for the same key")
	}
	if sharedMetaMissID(metaID, "") != metaID {
		t.Fatalf("empty api key should use bare meta id, got %q", sharedMetaMissID(metaID, ""))
	}
}

package stremio

import (
	"context"
	"testing"
)

func TestResolveSearchTerms(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	// Code-shape queries are searched literally, no provider call.
	if got := h.resolveSearchTerms(ctx, Config{}, "MIDA-574"); len(got) != 1 || got[0] != "MIDA-574" {
		t.Errorf("code query: got %v, want [MIDA-574]", got)
	}
	// Without a StashDB key, free text falls back to the raw title search only.
	if got := h.resolveSearchTerms(ctx, Config{}, "Mio Ishikawa"); len(got) != 1 || got[0] != "Mio Ishikawa" {
		t.Errorf("no-key free text: got %v, want [Mio Ishikawa]", got)
	}
	// Blank query yields nothing.
	if got := h.resolveSearchTerms(ctx, Config{}, "   "); got != nil {
		t.Errorf("blank query: got %v, want nil", got)
	}
}

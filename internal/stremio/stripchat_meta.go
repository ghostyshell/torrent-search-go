package stremio

import (
	"context"
	"strings"
)

// serveStripchatMeta returns a full Meta for an sc: id. The broadcasts API is
// consulted; an offline performer may 404 and return nil. A live performer
// resolves to a clickable meta (Node stream route serves HLS when live).
func (h *Handler) serveStripchatMeta(ctx context.Context, id string) (*Meta, error) {
	username := strings.TrimPrefix(id, "sc:")
	username = stripchatStripNil(username)
	if username == "" {
		return nil, nil
	}
	m, err := h.stripchatFetchCam(ctx, username)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	if m.Username == "" {
		m.Username = username
	}
	h.stripchatEnrichPreview(ctx, m)
	name := stripchatStripNil(m.Username)
	poster := stripchatPoster(*m)
	return &Meta{
		ID:          id,
		Type:        "Porn",
		Name:        name,
		Poster:      poster,
		Background:  poster,
		Description: stripchatDescription(*m),
		PosterShape: "landscape",
	}, nil
}

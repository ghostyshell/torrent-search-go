package stremio

import (
	"context"
	"strings"
)

// stripchatWebsite returns the canonical Stripchat profile URL for a username.
func stripchatWebsite(username string) string {
	return stripchatAPIBase + "/" + username
}

// serveStripchatMeta returns a full Meta for an sc: id. The cam endpoint is
// always consulted; an offline performer still resolves to a clickable meta
// (Node stream route then returns zero streams). A 404 - the user does not
// exist - returns nil so Stremio shows "No metadata found".
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
		Website:     stripchatWebsite(username),
		Links: []Link{
			{
				Name:     "Open on Stripchat",
				Category: "Genres",
				URL:      stripchatWebsite(username),
			},
		},
	}, nil
}

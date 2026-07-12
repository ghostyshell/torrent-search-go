package stremio

import "context"

func (h *Handler) resolveCover(ctx context.Context, rec TorrentRecord) string {
	if rec.CoverImage != "" {
		return rec.CoverImage
	}
	if h.Cover == nil {
		return ""
	}
	url, err := h.Cover.CoverURL(ctx, rec.Website, rec.Title, rec.Size)
	if err != nil || url == "" {
		return ""
	}
	return url
}

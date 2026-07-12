package stremio

// PornripsSlug extracts the post slug from a PornRips detail URL. Backs the
// Mongo-only pornrips meta path (entry lookup by slug) and the jstrm ID encoder.
func PornripsSlug(detailURL string) string {
	if m := pornripsSlugRE.FindStringSubmatch(detailURL); len(m) > 1 {
		return m[1]
	}
	return ""
}
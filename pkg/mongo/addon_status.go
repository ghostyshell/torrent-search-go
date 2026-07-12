package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
	"torrent-search-go/pkg/storage"
)

const addonStatusColl = "addon_status_reports"

// ListAddonStatusReports returns every managed addon status report, sorted by addon name.
func (c *Client) ListAddonStatusReports(ctx context.Context) ([]models.AddonStatusReport, error) {
	cur, err := c.coll(addonStatusColl).Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "addon.name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []models.AddonStatusReport
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return []models.AddonStatusReport{}, nil
	}
	return out, nil
}

// GetAddonStatusReport returns one report by addon id. mongo.ErrNoDocuments means not found.
func (c *Client) GetAddonStatusReport(ctx context.Context, id string) (*models.AddonStatusReport, error) {
	var r models.AddonStatusReport
	if err := c.coll(addonStatusColl).FindOne(ctx, bson.M{"_id": id}).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpsertAddonStatusReport replaces (or inserts) a whole report, keyed by Addon.ID.
func (c *Client) UpsertAddonStatusReport(ctx context.Context, r models.AddonStatusReport) error {
	r.ID = r.Addon.ID
	_, err := c.coll(addonStatusColl).ReplaceOne(ctx, bson.M{"_id": r.ID}, r, options.Replace().SetUpsert(true))
	return err
}

// DeleteAddonStatusReport removes a report by addon id. ErrAddonStatusNotFound means no match.
func (c *Client) DeleteAddonStatusReport(ctx context.Context, id string) error {
	res, err := c.coll(addonStatusColl).DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return storage.ErrAddonStatusNotFound
	}
	return nil
}

// seedAddonStatusReports inserts the initial TPB 4K Porn report when the collection is empty,
// so the public site has data on first deploy with no manual dashboard entry. Idempotent.
func (c *Client) seedAddonStatusReports(ctx context.Context) error {
	count, err := c.coll(addonStatusColl).CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = c.coll(addonStatusColl).InsertOne(ctx, seedTPB4KPornReport())
	return err
}

// seedTPB4KPornReport mirrors the hardcoded data previously baked into the adult-addons site
// (src/lib/tpb-status.ts + the FEATURES const in src/app/tpb-4k-porn/page.tsx).
func seedTPB4KPornReport() models.AddonStatusReport {
	return models.AddonStatusReport{
		ID: "tpb-4k-porn",
		Addon: models.AddonMeta{
			ID:        "tpb-4k-porn",
			Name:      "TPB 4K Porn",
			Status:    "LIVE",
			UpdatedAt: "2026-06-25",
		},
		Sources: []models.AddonSource{
			{ID: "tpb", Name: "TPB (HiddenBay)", Status: "LIVE", Detail: "4K, 1080p & VR torrent catalogs, JAV search."},
			{ID: "pornrips", Name: "Scenes (PornRips)", Status: "LIVE", Detail: "Scene releases with studio filters and TPDB metadata."},
			{ID: "hentai", Name: "Hentai", Status: "LIVE", Detail: "Hentai series streamed as direct video episodes."},
			{ID: "sukebei", Name: "Sukebei", Note: "Beta", Status: "LIVE", Detail: "Sukebei Nyaa torrents, top and recent sorts."},
			{ID: "stripchat", Name: "Stripchat", Note: "Beta", Status: "LIVE", Detail: "Live cam catalogs (Girls, Couples, Guys, Trans) via HLS proxy."},
			{ID: "tpdb", Name: "ThePornDB", Status: "LIVE", Detail: "Scene browser and optional TPDB/StashDB metadata."},
		},
		Issues: []models.AddonIssue{},
		Changelog: []models.AddonChangelog{
			{
				Version: "1.9.40",
				Date:    "2026-06-22",
				Highlights: []string{
					"Added Stripchat HLS proxy with automatic key extraction and multi-quality streams.",
					"Fixed saved-config auth so account settings load across browsers and pods.",
					"Fixed Stripchat playback by decrypting HLS segment URLs.",
				},
			},
			{
				Version: "1.9.21",
				Date:    "2026-06-22",
				Highlights: []string{
					"Added the Stripchat source with four live cam catalogs and username search.",
					"Added Account save/restore: AES-256-GCM encrypted addon settings in MongoDB.",
				},
			},
			{
				Version: "Unreleased",
				Date:    "In progress",
				Highlights: []string{
					"PornRips stream resolution moved to the Go backend for shared infoHash caching.",
					"Documented Stripchat network white-label domains in the configure page.",
				},
			},
		},
		Features: []models.AddonFeature{
			{Title: "4K, 1080p & VR torrent catalogs", Body: "Browse ThePirateBay and HiddenBay adult releases in 4K, 1080p, and VR, with JAV title and studio search baked into Discover."},
			{Title: "Debrid-ready playback", Body: "Real-Debrid, TorBox, Premiumize, and 9 more providers for cached, instant streams. P2P torrent playback works without a debrid account."},
			{Title: "Scene releases with metadata", Body: "PornRips scene catalogs with studio filters and optional ThePornDB and StashDB metadata for posters, cast, and tags."},
			{Title: "Hentai episodes", Body: "Hentai series streamed as direct video episodes, plus Sukebei Nyaa torrents (beta) with top and recent sorts."},
			{Title: "Stripchat live cams", Body: "Four live cam catalogs (Girls, Couples, Guys, Trans) with username search and multi-quality HLS via a dedicated proxy (beta)."},
			{Title: "Stremio and Nuvio compatible", Body: "Works in Stremio on desktop, mobile, and TV. Nuvio and other Stremio-addon clients can use the same manifest URL."},
		},
	}
}
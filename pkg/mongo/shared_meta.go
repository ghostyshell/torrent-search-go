package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// shared_meta is the durable home for resolved TPDB/StashDB metadata. Documents
// are keyed _id = source + ":" + metaID (e.g. "tpdb:<infohash>", "stashdb:pr:slug"),
// mirroring the Redis tpdb-shared:/stashdb-shared: keying so the DescriptionImageCache
// and MetaEnricher jobs can share a single source of truth and de-dupe lookups.

func sharedMetaDocID(source, metaID string) string {
	return source + ":" + metaID
}

// SetSharedMeta upserts a per-source resolved metadata payload.
func (c *Client) SetSharedMeta(ctx context.Context, source, metaID string, p models.SharedMetaPayload) error {
	if source == "" || metaID == "" {
		return nil
	}
	if p.UpdatedAt == 0 {
		p.UpdatedAt = nowSec()
	}
	doc := bson.M{
		"_id":         sharedMetaDocID(source, metaID),
		"meta_id":     metaID,
		"source":      source,
		"title":       p.Title,
		"description": p.Description,
		"poster":      p.Poster,
		"background":  p.Background,
		"year":        p.Year,
		"cast":        p.Cast,
		"tags":        p.Tags,
		"genres":      p.Genres,
		"updated_at":  p.UpdatedAt,
	}
	_, err := c.coll("shared_meta").ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	return err
}

// GetSharedMetaPair returns the TPDB and StashDB payloads for a metaID (either may be nil).
func (c *Client) GetSharedMetaPair(ctx context.Context, metaID string) (*models.SharedMetaPayload, *models.SharedMetaPayload, error) {
	if metaID == "" {
		return nil, nil, nil
	}
	cur, err := c.coll("shared_meta").Find(ctx, bson.M{
		"_id": bson.M{"$in": []string{
			sharedMetaDocID("tpdb", metaID),
			sharedMetaDocID("stashdb", metaID),
		}},
	})
	if err != nil {
		return nil, nil, err
	}
	defer cur.Close(ctx)

	var tpdb, stash *models.SharedMetaPayload
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		payload := mapSharedMeta(doc)
		switch strVal(doc, "source") {
		case "stashdb":
			stash = payload
		case "tpdb":
			tpdb = payload
		}
	}
	return tpdb, stash, cur.Err()
}

// ExistsSharedMany reports, per metaID, whether a payload exists for the given source.
func (c *Client) ExistsSharedMany(ctx context.Context, source string, metaIDs []string) ([]bool, error) {
	out := make([]bool, len(metaIDs))
	if source == "" || len(metaIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(metaIDs))
	idx := make(map[string]int, len(metaIDs))
	for i, m := range metaIDs {
		if m == "" {
			continue
		}
		docID := sharedMetaDocID(source, m)
		ids = append(ids, docID)
		idx[docID] = i
	}
	if len(ids) == 0 {
		return out, nil
	}
	cur, err := c.coll("shared_meta").Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return out, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		if id, ok := doc["_id"].(string); ok {
			if i, ok := idx[id]; ok {
				out[i] = true
			}
		}
	}
	return out, cur.Err()
}

func mapSharedMeta(doc bson.M) *models.SharedMetaPayload {
	return &models.SharedMetaPayload{
		Title:       strVal(doc, "title"),
		Description: strVal(doc, "description"),
		Poster:      strVal(doc, "poster"),
		Background:  strVal(doc, "background"),
		Year:        strVal(doc, "year"),
		Cast:        stringSlice(doc, "cast"),
		Tags:        stringSlice(doc, "tags"),
		Genres:      stringSlice(doc, "genres"),
		Source:      strVal(doc, "source"),
		UpdatedAt:   int64Val(doc, "updated_at"),
	}
}

// stringSlice decodes a bson array field into a []string.
func stringSlice(doc bson.M, key string) []string {
	raw, ok := doc[key].(bson.A)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

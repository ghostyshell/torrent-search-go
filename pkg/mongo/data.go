package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// coverUpsert runs UpdateOne with $setOnInsert (stable insert-only fields) and
// $set (the caller-supplied fields). This is a partial update — existing fields
// that are not in setFields are preserved, so tpdb_url / details_url survive
// when only the primary cover slot is being refreshed, and vice versa.
func (c *Client) coverUpsert(ctx context.Context, torrentKey string, setOnInsert, setFields bson.M) error {
	filter := bson.M{"_id": coverDocID(torrentKey)}
	update := bson.M{"$set": setFields}
	if len(setOnInsert) > 0 {
		update["$setOnInsert"] = setOnInsert
	}
	_, err := c.coll("images").UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (c *Client) SetCoverImage(ctx context.Context, torrentKey, imageURL string) error {
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		bson.M{"pixhost_url": imageURL, "original_url": imageURL},
	)
}

// SetCoverImageWithStorageKey stores a cover image URL together with an S3 object key.
func (c *Client) SetCoverImageWithStorageKey(ctx context.Context, torrentKey, imageURL, originalURL, storageKey string) error {
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		bson.M{"pixhost_url": imageURL, "original_url": originalURL, "storage_key": storageKey},
	)
}

// SetManualCover marks a cover as manually chosen by the user. Only the primary
// slot is updated; tpdb_url and details_url are left untouched.
func (c *Client) SetManualCover(ctx context.Context, torrentKey, imageURL, originalURL, storageKey string) error {
	if originalURL == "" {
		originalURL = imageURL
	}
	set := bson.M{"pixhost_url": imageURL, "original_url": originalURL, "cover_source": "manual"}
	if storageKey != "" {
		set["storage_key"] = storageKey
	}
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		set,
	)
}

// UpsertTpdbCover stores a TPDB/StashDB cover in its dedicated tpdb_url slot
// and updates the primary cover slot unless the user manually overrode it.
func (c *Client) UpsertTpdbCover(ctx context.Context, torrentKey, imageURL, originalURL, storageKey, source, description, metaID string) error {
	existing, _ := c.GetCoverImageByKey(ctx, torrentKey)
	isManual := existing != nil && existing.CoverSource != nil && *existing.CoverSource == "manual"

	set := bson.M{"tpdb_url": imageURL}
	if storageKey != "" {
		set["tpdb_storage_key"] = storageKey
	}
	if !isManual {
		set["pixhost_url"]  = imageURL
		set["original_url"] = originalURL
		set["cover_source"] = source
		if storageKey != "" {
			set["storage_key"] = storageKey
		}
		if description != "" {
			set["description"] = description
		}
		if metaID != "" {
			set["meta_id"] = metaID
		}
	}
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		set,
	)
}

// UpsertDetailsCover stores a description/NFO scrape cover in its dedicated
// details_url slot and populates the primary slot only when no better cover exists.
func (c *Client) UpsertDetailsCover(ctx context.Context, torrentKey, imageURL, storageKey string) error {
	existing, _ := c.GetCoverImageByKey(ctx, torrentKey)
	noPrimary := existing == nil || existing.PixhostURL == ""

	set := bson.M{"details_url": imageURL}
	if storageKey != "" {
		set["details_storage_key"] = storageKey
	}
	if noPrimary {
		set["pixhost_url"]  = imageURL
		set["original_url"] = imageURL
		set["cover_source"] = "description"
	}
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		set,
	)
}

// SetCoverImageMeta is retained for backward compatibility. New callers should
// use UpsertTpdbCover or UpsertDetailsCover instead.
func (c *Client) SetCoverImageMeta(ctx context.Context, torrentKey, imageURL, originalURL, storageKey, source, description, metaID string) error {
	if originalURL == "" {
		originalURL = imageURL
	}
	set := bson.M{
		"pixhost_url":  imageURL,
		"original_url": originalURL,
	}
	if storageKey != "" {
		set["storage_key"] = storageKey
	}
	if source != "" {
		set["cover_source"] = source
	}
	if description != "" {
		set["description"] = description
	}
	if metaID != "" {
		set["meta_id"] = metaID
	}
	return c.coverUpsert(ctx, torrentKey,
		bson.M{"torrent_key": torrentKey, "image_type": "cover", "created_at": nowSec()},
		set,
	)
}

func (c *Client) GetCoverImageByKey(ctx context.Context, torrentKey string) (*models.ImageRow, error) {
	var doc bson.M
	err := c.coll("images").FindOne(ctx, bson.M{"_id": coverDocID(torrentKey)}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapImage(doc), nil
}

func (c *Client) DeleteCoverImage(ctx context.Context, torrentKey string) (bool, error) {
	if torrentKey == "" {
		return false, nil
	}
	res, err := c.coll("images").DeleteOne(ctx, bson.M{"_id": coverDocID(torrentKey)})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (c *Client) GetCoverImagesByKeys(ctx context.Context, torrentKeys []string) (map[string]*models.ImageRow, error) {
	out := make(map[string]*models.ImageRow)
	if len(torrentKeys) == 0 {
		return out, nil
	}
	cur, err := c.coll("images").Find(ctx, bson.M{
		"image_type":  "cover",
		"torrent_key": bson.M{"$in": torrentKeys},
	})
	if err != nil {
		return out, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		key := strVal(doc, "torrent_key")
		out[key] = mapImage(doc)
	}
	return out, cur.Err()
}

func mapImage(doc bson.M) *models.ImageRow {
	row := &models.ImageRow{
		TorrentKey: strVal(doc, "torrent_key"),
		PixhostURL: strVal(doc, "pixhost_url"),
	}
	if o := strVal(doc, "original_url"); o != "" {
		row.OriginalURL = &o
	}
	if s := strVal(doc, "storage_key"); s != "" {
		row.StorageKey = &s
	}
	if d := strVal(doc, "description"); d != "" {
		row.Description = &d
	}
	if cs := strVal(doc, "cover_source"); cs != "" {
		row.CoverSource = &cs
	}
	if m := strVal(doc, "meta_id"); m != "" {
		row.MetaID = &m
	}
	if v := strVal(doc, "tpdb_url"); v != "" {
		row.TpdbURL = &v
	}
	if v := strVal(doc, "tpdb_storage_key"); v != "" {
		row.TpdbStorageKey = &v
	}
	if v := strVal(doc, "details_url"); v != "" {
		row.DetailsURL = &v
	}
	if v := strVal(doc, "details_storage_key"); v != "" {
		row.DetailsStorageKey = &v
	}
	return row
}

// GetObjectStorageCovers returns cover rows that have an associated S3 storage key.
func (c *Client) GetObjectStorageCovers(ctx context.Context, limit, offset int) ([]models.ObjectStorageCover, error) {
	out := make([]models.ObjectStorageCover, 0)
	cur, err := c.coll("images").Find(ctx, bson.M{
		"image_type":  "cover",
		"storage_key": bson.M{"$ne": nil},
	}, options.Find().
		SetSort(bson.D{{Key: "torrent_key", Value: 1}}).
		SetSkip(int64(offset)).
		SetLimit(int64(limit)).
		SetProjection(bson.M{"torrent_key": 1, "storage_key": 1, "_id": 0}))
	if err != nil {
		return out, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, models.ObjectStorageCover{
			TorrentKey: strVal(doc, "torrent_key"),
			StorageKey: strVal(doc, "storage_key"),
		})
	}
	return out, cur.Err()
}

// UpdateCoverPresignedURL refreshes the stored presigned URL for a cover row.
func (c *Client) UpdateCoverPresignedURL(ctx context.Context, torrentKey, presignedURL string) (bool, error) {
	res, err := c.coll("images").UpdateOne(ctx,
		bson.M{"_id": coverDocID(torrentKey)},
		bson.M{"$set": bson.M{"pixhost_url": presignedURL}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// GetCoverImagesMissingStorageKey returns cover rows that have a pixhost_url but
// no S3 storage_key — i.e. covers stored as raw external URLs that can't be
// re-signed. Used by the cover-storage backfill to upload them into object storage.
// afterKey enables keyset paging: pass "" for the first page, then the last
// torrent_key of the previous page. Keyset (not skip-offset) is required because
// successful backfills mutate the matched set, which would break skip paging.
func (c *Client) GetCoverImagesMissingStorageKey(ctx context.Context, limit int, afterKey string) ([]*models.ImageRow, error) {
	out := make([]*models.ImageRow, 0)
	filter := bson.M{
		"image_type":  "cover",
		"pixhost_url": bson.M{"$exists": true, "$ne": ""},
		"$or": []bson.M{
			{"storage_key": bson.M{"$exists": false}},
			{"storage_key": ""},
			{"storage_key": nil},
		},
	}
	if afterKey != "" {
		filter["torrent_key"] = bson.M{"$gt": afterKey}
	}
	cur, err := c.coll("images").Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "torrent_key", Value: 1}}).
		SetLimit(int64(limit)).
		SetProjection(bson.M{"torrent_key": 1, "pixhost_url": 1, "original_url": 1, "cover_source": 1, "meta_id": 1, "_id": 0}))
	if err != nil {
		return out, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapImage(doc))
	}
	return out, cur.Err()
}

// UpdateCoverStorageKey sets the S3 storage_key and a fresh presigned URL on an
// existing cover row, after the image has been uploaded to object storage.
func (c *Client) UpdateCoverStorageKey(ctx context.Context, torrentKey, storageKey, presignedURL string) (bool, error) {
	res, err := c.coll("images").UpdateOne(ctx,
		bson.M{"_id": coverDocID(torrentKey)},
		bson.M{"$set": bson.M{"storage_key": storageKey, "pixhost_url": presignedURL}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// DeleteCoverByStorageKey deletes the cover row with the given S3 object key.
func (c *Client) UpdateTorrentDetailsCoverImage(ctx context.Context, favoriteID, source, coverImageURL string) (bool, error) {
	docID := favoriteID + "::" + source
	res, err := c.coll("torrent_details").UpdateOne(ctx, bson.M{"_id": docID}, bson.M{"$set": bson.M{
		"cover_image_url": coverImageURL,
		"updated_at":      nowSec(),
	}})
	return res.ModifiedCount > 0, err
}

func (c *Client) UpdateCachedLinkCoverImage(ctx context.Context, cachedLinkID, coverImageURL string) (bool, error) {
	res, err := c.coll("cached_links").UpdateOne(ctx, bson.M{"_id": cachedLinkID}, bson.M{"$set": bson.M{
		"cover_image_url": coverImageURL,
	}})
	return res.ModifiedCount > 0, err
}

func (c *Client) DeleteCoverByStorageKey(ctx context.Context, storageKey string) (bool, error) {
	res, err := c.coll("images").DeleteOne(ctx, bson.M{"image_type": "cover", "storage_key": storageKey})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (c *Client) SetStreamURL(ctx context.Context, in models.StreamURLInput) error {
	now := nowSec()
	// Mirror the JS stream_urls schema: store nullable filename/filesize/
	// torrent_name and a 0/1 supports_range_requests flag.
	doc := bson.M{
		"_id":                     in.MagnetHash,
		"magnet_hash":             in.MagnetHash,
		"stream_url":              in.StreamURL,
		"filename":                nilIfEmptyStr(in.Filename),
		"filesize":                nilIfZeroInt64(in.Filesize),
		"supports_range_requests": boolToInt(in.SupportsRangeRequests),
		"torrent_name":            nilIfEmptyStr(in.TorrentName),
		"created_at":              now,
		"last_accessed_at":        now,
	}
	if in.MagnetLink != "" {
		doc["magnet_link"] = in.MagnetLink
	}
	_, err := c.coll("stream_urls").ReplaceOne(ctx, bson.M{"_id": in.MagnetHash}, doc, options.Replace().SetUpsert(true))
	return err
}

func (c *Client) GetStreamURLByHash(ctx context.Context, magnetHash string) (*models.StreamURLRow, error) {
	filter := bson.M{"_id": magnetHash}
	if c.streamURLTTL > 0 {
		filter["created_at"] = bson.M{"$gt": nowSec() - c.streamURLTTL}
	}
	var doc bson.M
	err := c.coll("stream_urls").FindOne(ctx, filter).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	accessed := nowSec()
	_, _ = c.coll("stream_urls").UpdateOne(ctx, bson.M{"_id": magnetHash}, bson.M{"$set": bson.M{"last_accessed_at": accessed}})
	return &models.StreamURLRow{
		MagnetHash:            magnetHash,
		MagnetLink:            strVal(doc, "magnet_link"),
		StreamURL:             strVal(doc, "stream_url"),
		Filename:              strVal(doc, "filename"),
		Filesize:              int64Val(doc, "filesize"),
		SupportsRangeRequests: int64Val(doc, "supports_range_requests") != 0,
		TorrentName:           strVal(doc, "torrent_name"),
		CreatedAt:             int64Val(doc, "created_at"),
		LastAccessedAt:        accessed,
	}, nil
}

// nilIfEmptyStr returns nil for an empty string so the stored field is null
// (matching the JS `value || null` persistence pattern).
func nilIfEmptyStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nilIfZeroInt64 returns nil for a zero value so the stored field is null.
func nilIfZeroInt64(n int64) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

// boolToInt converts a bool to the 0/1 integer the JS schema uses.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *Client) GetStreamURLByMagnet(ctx context.Context, magnetLink string) (*models.StreamURLRow, error) {
	return c.GetStreamURLByHash(ctx, extractMagnetHash(magnetLink))
}

func (c *Client) AddCachedLink(ctx context.Context, id, userID, _, originalURL, title string) error {
	doc := bson.M{
		"_id":        id,
		"id":         id,
		"url":        originalURL,
		"title":      title,
		"date_added": time.Now().UTC().Format(time.RFC3339),
	}
	if userID != "" {
		doc["user_id"] = userID
	}
	_, err := c.coll("cached_links").ReplaceOne(ctx, bson.M{"_id": id}, doc, options.Replace().SetUpsert(true))
	return err
}

func (c *Client) GetCachedLinks(ctx context.Context, page, limit int, userID string) ([]*models.CachedLinkRow, int, error) {
	filter := userIDFilter(userID)
	total64, err := c.coll("cached_links").CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	cur, err := c.coll("cached_links").Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "date_added", Value: -1}}).
		SetSkip(int64(offset)).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	var rows []*models.CachedLinkRow
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		rows = append(rows, mapCachedLink(doc))
	}
	return rows, int(total64), cur.Err()
}

func (c *Client) GetCachedLinkByID(ctx context.Context, id string) (*models.CachedLinkRow, error) {
	var doc bson.M
	err := c.coll("cached_links").FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapCachedLink(doc), nil
}

func mapCachedLink(doc bson.M) *models.CachedLinkRow {
	row := &models.CachedLinkRow{
		ID:        strVal(doc, "id"),
		URL:       strVal(doc, "url"),
		DateAdded: strVal(doc, "date_added"),
	}
	if t := strVal(doc, "title"); t != "" {
		row.Title = &t
	}
	if s := strVal(doc, "stream_url"); s != "" {
		row.StreamURL = &s
	}
	if e := strVal(doc, "error"); e != "" {
		row.Error = &e
	}
	if u := strVal(doc, "user_id"); u != "" {
		row.UserID = &u
	}
	if c := strVal(doc, "cover_image_url"); c != "" {
		row.CoverImageURL = &c
	}
	if v, ok := doc["is_streaming"]; ok {
		switch n := v.(type) {
		case bool:
			row.IsStreaming = n
		case int32, int64, int, float64:
			row.IsStreaming = int64Val(doc, "is_streaming") == 1
		}
	}
	return row
}

func (c *Client) UpdateCachedLink(ctx context.Context, id, userID string, updates map[string]interface{}) (bool, error) {
	set := bson.M{}
	fieldMap := map[string]string{
		"title": "title", "streamUrl": "stream_url", "streamUrlCachedAt": "stream_url_cached_at",
		"isStreaming": "is_streaming", "error": "error", "supportsRangeRequests": "supports_range_requests",
		"filename": "filename", "coverImageUrl": "cover_image_url",
	}
	for in, out := range fieldMap {
		if v, ok := updates[in]; ok {
			set[out] = v
		}
	}
	if len(set) == 0 {
		return false, nil
	}
	filter := bson.M{"_id": id}
	for k, v := range userIDFilter(userID) {
		filter[k] = v
	}
	res, err := c.coll("cached_links").UpdateOne(ctx, filter, bson.M{"$set": set})
	return res.ModifiedCount > 0, err
}

func (c *Client) RemoveCachedLink(ctx context.Context, id, userID string) (bool, error) {
	filter := bson.M{"_id": id}
	for k, v := range userIDFilter(userID) {
		filter[k] = v
	}
	res, err := c.coll("cached_links").DeleteOne(ctx, filter)
	return res.DeletedCount > 0, err
}

func (c *Client) KVSet(ctx context.Context, key, value string, ttlSeconds *int64) error {
	now := nowSec()
	doc := bson.M{
		"_id":        key,
		"key":        key,
		"value":      value,
		"type":       "string",
		"created_at": now,
		"updated_at": now,
	}
	if ttlSeconds != nil {
		doc["expires_at"] = now + *ttlSeconds
	}
	_, err := c.coll("cache").ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (c *Client) KVGet(ctx context.Context, key string) (string, bool, error) {
	now := nowSec()
	var doc bson.M
	err := c.coll("cache").FindOne(ctx, bson.M{
		"_id": key,
		"$or": []bson.M{
			{"expires_at": nil},
			{"expires_at": bson.M{"$exists": false}},
			{"expires_at": bson.M{"$gt": now}},
		},
	}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strVal(doc, "value"), true, nil
}

func (c *Client) KVDelete(ctx context.Context, key string) error {
	_, err := c.coll("cache").DeleteOne(ctx, bson.M{"_id": key})
	return err
}

package mongo

import (
	"context"
	"encoding/json"
	"sort"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

func mapFavorite(doc bson.M) *models.FavoriteRow {
	raw, _ := doc["torrent_data"].(string)
	return &models.FavoriteRow{
		ID:            strVal(doc, "id"),
		UserID:        strVal(doc, "user_id"),
		TorrentKey:    strVal(doc, "torrent_key"),
		TorrentName:   strVal(doc, "torrent_name"),
		Website:       websiteFromTorrentData(raw),
		TorrentData:   raw,
		MagnetLink:    strVal(doc, "magnet_link"),
		CoverImageURL: strVal(doc, "cover_image_url"),
		CreatedAt:     int64Val(doc, "created_at"),
		UpdatedAt:     int64Val(doc, "updated_at"),
	}
}

func (c *Client) fetchFavoriteRows(ctx context.Context, filter bson.M, limit, offset int) ([]*models.FavoriteRow, error) {
	// Node getMergedFavorites: dedupe by magnet_link || id, keep newest first
	cur, err := c.coll("favorite_entries").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	seen := make(map[string]bool)
	var rows []*models.FavoriteRow
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		key := strVal(doc, "magnet_link")
		if key == "" {
			key = strVal(doc, "id")
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, mapFavorite(doc))
	}
	if offset >= len(rows) {
		return []*models.FavoriteRow{}, nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end], nil
}

func (c *Client) countDedupedFavorites(ctx context.Context, filter bson.M) (int, error) {
	cur, err := c.coll("favorite_entries").Find(ctx, filter, options.Find().SetProjection(bson.M{"magnet_link": 1, "id": 1}))
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	seen := make(map[string]bool)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		key := strVal(doc, "magnet_link")
		if key == "" {
			key = strVal(doc, "id")
		}
		seen[key] = true
	}
	return len(seen), cur.Err()
}

func (c *Client) AddFavorite(ctx context.Context, id, userID, torrentKey, torrentName, torrentData, coverImageURL, magnetLink string) error {
	now := nowSec()
	filter := bson.M{"torrent_key": torrentKey}
	filter["user_id"] = nil
	if userID != "" {
		filter["user_id"] = userID
	}
	_, _ = c.coll("favorite_entries").DeleteOne(ctx, filter)

	doc := bson.M{
		"_id":          id,
		"id":           id,
		"torrent_key":  torrentKey,
		"torrent_data": torrentData,
		"torrent_name": torrentName,
		"created_at":   now,
		"updated_at":   now,
	}
	if userID != "" {
		doc["user_id"] = userID
	}
	if coverImageURL != "" {
		doc["cover_image_url"] = coverImageURL
	}
	if magnetLink != "" {
		doc["magnet_link"] = magnetLink
	}
	_, err := c.coll("favorite_entries").InsertOne(ctx, doc)
	return err
}

func (c *Client) GetFavoritesByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.FavoriteRow, error) {
	return c.fetchFavoriteRows(ctx, userIDFilter(userID), limit, offset)
}

func (c *Client) GetFavoritesByUserIDs(ctx context.Context, userIDs []string, limit, offset int) ([]*models.FavoriteRow, error) {
	return c.fetchFavoriteRows(ctx, userIDsFilter(userIDs), limit, offset)
}

func (c *Client) CountFavoritesByUserID(ctx context.Context, userID string) (int, error) {
	return c.countDedupedFavorites(ctx, userIDFilter(userID))
}

func (c *Client) CountFavoritesByUserIDs(ctx context.Context, userIDs []string) (int, error) {
	return c.countDedupedFavorites(ctx, userIDsFilter(userIDs))
}

func (c *Client) RemoveFavorite(ctx context.Context, torrentKey, userID string) (bool, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDFilter(userID) {
		filter[k] = v
	}
	res, err := c.coll("favorite_entries").DeleteOne(ctx, filter)
	return res.DeletedCount > 0, err
}

func (c *Client) RemoveFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDsFilter(userIDs) {
		filter[k] = v
	}
	res, err := c.coll("favorite_entries").DeleteOne(ctx, filter)
	return res.DeletedCount > 0, err
}

func (c *Client) IsFavorite(ctx context.Context, torrentKey, userID string) (bool, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDFilter(userID) {
		filter[k] = v
	}
	n, err := c.coll("favorite_entries").CountDocuments(ctx, filter)
	return n > 0, err
}

func (c *Client) IsFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDsFilter(userIDs) {
		filter[k] = v
	}
	n, err := c.coll("favorite_entries").CountDocuments(ctx, filter)
	return n > 0, err
}

func (c *Client) GetFavoriteByKey(ctx context.Context, torrentKey, userID string) (*models.FavoriteRow, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDFilter(userID) {
		filter[k] = v
	}
	var doc bson.M
	err := c.coll("favorite_entries").FindOne(ctx, filter).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapFavorite(doc), nil
}

func (c *Client) GetFavoriteByKeyForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (*models.FavoriteRow, error) {
	filter := bson.M{"torrent_key": torrentKey}
	for k, v := range userIDsFilter(userIDs) {
		filter[k] = v
	}
	var doc bson.M
	err := c.coll("favorite_entries").FindOne(ctx, filter).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapFavorite(doc), nil
}

func (c *Client) GetFavoriteEntryByID(ctx context.Context, entryID string) (interface{}, error) {
	var doc bson.M
	err := c.coll("favorite_entries").FindOne(ctx, bson.M{"_id": entryID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *Client) UpdateFavoriteEntryCoverImage(ctx context.Context, entryID, userID, coverImageURL string) (bool, error) {
	filter := bson.M{"_id": entryID}
	if userID != "" {
		filter["user_id"] = userID
	}
	res, err := c.coll("favorite_entries").UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"cover_image_url": coverImageURL,
		"updated_at":      nowSec(),
	}})
	return res.ModifiedCount > 0, err
}

func (c *Client) UpdateFavoriteEntryMagnetLink(ctx context.Context, entryID, userID, magnetLink string) (bool, error) {
	filter := bson.M{"_id": entryID}
	if userID != "" {
		filter["user_id"] = userID
	}
	res, err := c.coll("favorite_entries").UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"magnet_link": magnetLink,
		"updated_at":  nowSec(),
	}})
	return res.ModifiedCount > 0, err
}

func (c *Client) StoreFavoriteEntry(ctx context.Context, entryID string, data map[string]interface{}) error {
	raw, _ := json.Marshal(data)
	_, err := c.coll("favorite_entries").UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": bson.M{
		"metadata":   string(raw),
		"updated_at": nowSec(),
	}})
	return err
}

func (c *Client) StoreFavoriteDetails(ctx context.Context, favoriteID string, details interface{}) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)
	source := "default"
	if s, ok := payload["source"].(string); ok && s != "" {
		source = s
	}
	docID := favoriteID + "::" + source
	now := nowSec()
	doc := bson.M{
		"_id":               docID,
		"favorite_entry_id": favoriteID,
		"source":            source,
		"updated_at":        now,
		"created_at":        now,
	}
	for _, k := range []string{"details_url", "description", "cover_image_url", "error_message"} {
		if v, ok := payload[k]; ok {
			doc[k] = v
		}
	}
	for _, k := range []string{"files", "comments", "images"} {
		if v, ok := payload[k]; ok {
			b, _ := json.Marshal(v)
			doc[k] = string(b)
		}
	}
	_, err = c.coll("torrent_details").ReplaceOne(ctx, bson.M{"_id": docID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (c *Client) GetFavoriteDetails(ctx context.Context, favoriteID string) (interface{}, error) {
	cur, err := c.coll("torrent_details").Find(ctx, bson.M{"favorite_entry_id": favoriteID}, options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []map[string]interface{}
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		row := map[string]interface{}{
			"favoriteEntryId": doc["favorite_entry_id"],
			"source":          doc["source"],
			"detailsUrl":      doc["details_url"],
			"description":     doc["description"],
			"coverImageUrl":   doc["cover_image_url"],
			"error":           doc["error_message"],
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return rows, cur.Err()
}

func (c *Client) GetFavoritesForStreamRefresh(ctx context.Context) ([]models.UserFavoritesRefresh, error) {
	cur, err := c.coll("favorite_entries").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	seen := make(map[string]bool)
	grouped := make(map[string][]models.FavoriteRefreshItem)
	var order []string

	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		magnet := strVal(doc, "magnet_link")
		name := strVal(doc, "torrent_name")
		if magnet == "" {
			if raw, ok := doc["torrent_data"].(string); ok {
				var td map[string]interface{}
				if json.Unmarshal([]byte(raw), &td) == nil {
					if m, ok := td["Magnet"].(string); ok {
						magnet = m
					}
					if n, ok := td["Name"].(string); ok && name == "" {
						name = n
					}
				}
			}
		}
		if magnet == "" {
			continue
		}
		uid := strVal(doc, "user_id")
		dedupe := uid + "::" + magnet
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		if _, ok := grouped[uid]; !ok {
			order = append(order, uid)
		}
		grouped[uid] = append(grouped[uid], models.FavoriteRefreshItem{
			ID: strVal(doc, "id"), MagnetLink: magnet, TorrentName: name,
		})
	}

	sort.Strings(order)
	out := make([]models.UserFavoritesRefresh, 0, len(order))
	for _, uid := range order {
		out = append(out, models.UserFavoritesRefresh{UserID: uid, Favorites: grouped[uid]})
	}
	return out, cur.Err()
}

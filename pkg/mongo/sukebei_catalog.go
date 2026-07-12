package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SetSukebeiCatalog persists a resolved Sukebei catalog page to Mongo.
func (c *Client) SetSukebeiCatalog(ctx context.Context, catalogID string, entriesJSON []byte) error {
	if catalogID == "" || len(entriesJSON) == 0 {
		return nil
	}
	doc := bson.M{
		"_id":        "sukebei:" + catalogID,
		"catalog_id": catalogID,
		"entries":    string(entriesJSON),
		"updated_at": time.Now().Unix(),
	}
	_, err := c.coll("sukebei_catalog").ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	return err
}

// GetSukebeiCatalog loads a persisted Sukebei catalog blob.
func (c *Client) GetSukebeiCatalog(ctx context.Context, catalogID string) ([]byte, bool, error) {
	if catalogID == "" {
		return nil, false, nil
	}
	var doc bson.M
	err := c.coll("sukebei_catalog").FindOne(ctx, bson.M{"_id": "sukebei:" + catalogID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, false, nil
		}
		return nil, false, err
	}
	raw, ok := doc["entries"].(string)
	if !ok || raw == "" {
		return nil, false, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, false, nil
	}
	return []byte(raw), true, nil
}

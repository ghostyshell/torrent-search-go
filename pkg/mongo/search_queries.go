package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RecordSearchQuery upserts a row in the search_queries collection.
// The query text is used as the primary key so duplicate searches only update
// the updated_at timestamp. created_at is preserved on insert.
func (c *Client) RecordSearchQuery(ctx context.Context, query string) error {
	now := nowSec()
	_, err := c.coll("search_queries").UpdateOne(
		ctx,
		bson.M{"_id": query},
		bson.M{
			"$set": bson.M{
				"query":      query,
				"updated_at": now,
			},
			"$setOnInsert": bson.M{
				"created_at": now,
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

// GetRecentSearchQueries returns distinct query texts that have been updated
// within the requested retention window.
func (c *Client) GetRecentSearchQueries(ctx context.Context, since time.Time) ([]string, error) {
	filter := bson.M{"updated_at": bson.M{"$gte": since.Unix()}}
	raw, err := c.coll("search_queries").Distinct(ctx, "query", filter)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// CleanupOldSearchQueries deletes search_queries rows older than the cutoff.
func (c *Client) CleanupOldSearchQueries(ctx context.Context, before time.Time) (int64, error) {
	res, err := c.coll("search_queries").DeleteMany(ctx, bson.M{"updated_at": bson.M{"$lt": before.Unix()}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

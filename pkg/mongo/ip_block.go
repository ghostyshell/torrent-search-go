package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

func (c *Client) AddBlockedIP(ctx context.Context, ip, reason, notes string, requestCount int64) error {
	now := time.Now().UnixMilli()
	doc := bson.M{
		"_id":           ip,
		"reason":        reason,
		"notes":         notes,
		"request_count": requestCount,
		"blocked_at":    now,
		"is_active":     true,
		"updated_at":    now,
	}
	_, err := c.coll("blocked_ips").ReplaceOne(ctx, bson.M{"_id": ip}, doc, options.Replace().SetUpsert(true))
	return err
}

func (c *Client) RemoveBlockedIP(ctx context.Context, ip string) error {
	_, err := c.coll("blocked_ips").DeleteOne(ctx, bson.M{"_id": ip})
	return err
}

func (c *Client) GetBlockedIPs(ctx context.Context) ([]*models.BlockedIP, error) {
	cur, err := c.coll("blocked_ips").Find(ctx, bson.M{"is_active": true}, options.Find().SetSort(bson.D{{Key: "blocked_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []*models.BlockedIP
	for cur.Next(ctx) {
		var row models.BlockedIP
		if err := cur.Decode(&row); err == nil {
			out = append(out, &row)
		}
	}
	return out, cur.Err()
}

func (c *Client) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	count, err := c.coll("blocked_ips").CountDocuments(ctx, bson.M{"_id": ip, "is_active": true})
	return count > 0, err
}

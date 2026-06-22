package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

func mapUser(doc bson.M) *models.UserRow {
	if doc == nil {
		return nil
	}
	u := &models.UserRow{
		ID:        strVal(doc, "id"),
		Email:     strVal(doc, "email"),
		Name:      strVal(doc, "name"),
		CreatedAt: int64Val(doc, "created_at"),
		UpdatedAt: int64Val(doc, "updated_at"),
		IsActive:  int(int64Val(doc, "is_active")),
	}
	if p := strVal(doc, "picture"); p != "" {
		u.Picture = &p
	}
	if g := strVal(doc, "google_id"); g != "" {
		u.GoogleID = &g
	}
	if rd := strVal(doc, "real_debrid_api_key"); rd != "" {
		u.RealDebridAPIKey = &rd
	}
	if ll := int64Val(doc, "last_login_at"); ll > 0 {
		u.LastLoginAt = &ll
	}
	return u
}

func strVal(doc bson.M, key string) string {
	if v, ok := doc[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func int64Val(doc bson.M, key string) int64 {
	if v, ok := doc[key]; ok && v != nil {
		switch n := v.(type) {
		case int64:
			return n
		case int32:
			return int64(n)
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return 0
}

func (c *Client) GetUserByID(ctx context.Context, id string) (*models.UserRow, error) {
	var doc bson.M
	err := c.coll("users").FindOne(ctx, bson.M{"_id": id, "is_active": 1}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUser(doc), nil
}

func (c *Client) GetUserByEmail(ctx context.Context, email string) (*models.UserRow, error) {
	var doc bson.M
	err := c.coll("users").FindOne(ctx, bson.M{"email": strings.ToLower(email), "is_active": 1}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUser(doc), nil
}

func (c *Client) GetUserByGoogleID(ctx context.Context, googleID string) (*models.UserRow, error) {
	var doc bson.M
	err := c.coll("users").FindOne(ctx, bson.M{"google_id": googleID, "is_active": 1}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUser(doc), nil
}

func (c *Client) CreateUser(ctx context.Context, id, email, name, picture, googleID string) error {
	now := nowSec()
	doc := bson.M{
		"_id":           id,
		"id":            id,
		"email":         strings.ToLower(email),
		"name":          name,
		"picture":       picture,
		"google_id":     googleID,
		"created_at":    now,
		"updated_at":    now,
		"last_login_at": now,
		"is_active":     1,
	}
	_, err := c.coll("users").ReplaceOne(ctx, bson.M{"_id": id}, doc, optionsUpsert())
	return err
}

func optionsUpsert() *options.ReplaceOptions {
	return options.Replace().SetUpsert(true)
}

func (c *Client) UpdateUserLastLogin(ctx context.Context, id string) error {
	now := nowSec()
	_, err := c.coll("users").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"last_login_at": now, "updated_at": now}})
	return err
}

func (c *Client) UpdateUserGoogleTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt int64) error {
	now := nowSec()
	_, err := c.coll("users").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"google_access_token":     accessToken,
		"google_refresh_token":    refreshToken,
		"google_token_expires_at": expiresAt,
		"updated_at":              now,
	}})
	return err
}

func (c *Client) GetRealDebridKey(ctx context.Context, userID string) (string, error) {
	var doc bson.M
	err := c.coll("users").FindOne(ctx, bson.M{"_id": userID}, optionsOnly("real_debrid_api_key")).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strVal(doc, "real_debrid_api_key"), nil
}

func (c *Client) SetRealDebridKey(ctx context.Context, userID, apiKey string) error {
	_, err := c.coll("users").UpdateOne(ctx, bson.M{"_id": userID}, bson.M{"$set": bson.M{
		"real_debrid_api_key": apiKey,
		"updated_at":          nowSec(),
	}})
	return err
}

func (c *Client) DeleteRealDebridKey(ctx context.Context, userID string) error {
	_, err := c.coll("users").UpdateOne(ctx, bson.M{"_id": userID}, bson.M{"$set": bson.M{
		"real_debrid_api_key": nil,
		"updated_at":          nowSec(),
	}})
	return err
}

func (c *Client) GetUsersWithRealDebridKeys(ctx context.Context) ([]models.UserRealDebridKey, error) {
	cur, err := c.coll("users").Find(ctx, bson.M{
		"real_debrid_api_key": bson.M{"$nin": []interface{}{nil, ""}},
		"is_active":           1,
	}, options.Find().SetProjection(bson.M{"_id": 0, "id": 1, "real_debrid_api_key": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []models.UserRealDebridKey
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		key := strVal(doc, "real_debrid_api_key")
		if key == "" {
			continue
		}
		id := strVal(doc, "id")
		if id == "" {
			id = strVal(doc, "_id")
		}
		out = append(out, models.UserRealDebridKey{UserID: id, EncryptedKey: key})
	}
	return out, cur.Err()
}

func optionsOnly(fields ...string) *options.FindOneOptions {
	proj := bson.M{"_id": 0}
	for _, f := range fields {
		proj[f] = 1
	}
	return options.FindOne().SetProjection(proj)
}

func (c *Client) CreateExchangeCode(ctx context.Context, sessionToken string) (string, error) {
	code := newID()
	now := nowSec()
	_, err := c.coll("auth_exchange_codes").InsertOne(ctx, bson.M{
		"_id":           code,
		"session_token": sessionToken,
		"expires_at":    now + 120,
		"used":          false,
		"created_at":    now,
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

func (c *Client) ConsumeExchangeCode(ctx context.Context, code string) (string, error) {
	var doc bson.M
	err := c.coll("auth_exchange_codes").FindOne(ctx, bson.M{"_id": code}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	now := nowSec()
	if int64Val(doc, "expires_at") <= now {
		return "", nil
	}
	token := strVal(doc, "session_token")
	if used, _ := doc["used"].(bool); used {
		usedAt := int64Val(doc, "used_at")
		if now-usedAt > 60 {
			return "", nil
		}
		return token, nil
	}
	_, err = c.coll("auth_exchange_codes").UpdateOne(ctx,
		bson.M{"_id": code, "used": false},
		bson.M{"$set": bson.M{"used": true, "used_at": now}},
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (c *Client) CreateSession(ctx context.Context, sessionID, userID, token, userAgent, ipAddress string, expiresAt int64) error {
	now := nowSec()
	_, err := c.coll("user_sessions").InsertOne(ctx, bson.M{
		"_id":              sessionID,
		"id":               sessionID,
		"user_id":          userID,
		"session_token":    token,
		"expires_at":       expiresAt,
		"user_agent":       userAgent,
		"ip_address":       ipAddress,
		"created_at":       now,
		"last_accessed_at": now,
	})
	return err
}

func (c *Client) ValidateSession(ctx context.Context, token string) (*models.SessionRow, error) {
	var sess bson.M
	err := c.coll("user_sessions").FindOne(ctx, bson.M{
		"session_token": token,
		"expires_at":    bson.M{"$gt": nowSec()},
	}).Decode(&sess)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	userID := strVal(sess, "user_id")
	user, err := c.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}

	sessionID := strVal(sess, "id")
	_, _ = c.coll("user_sessions").UpdateOne(ctx, bson.M{"_id": sessionID}, bson.M{"$set": bson.M{"last_accessed_at": nowSec()}})

	row := &models.SessionRow{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: int64Val(sess, "expires_at"),
		CreatedAt: int64Val(sess, "created_at"),
		Email:     user.Email,
		Name:      user.Name,
		Picture:   user.Picture,
	}
	if ua := strVal(sess, "user_agent"); ua != "" {
		row.UserAgent = &ua
	}
	if ip := strVal(sess, "ip_address"); ip != "" {
		row.IPAddress = &ip
	}
	if la := int64Val(sess, "last_accessed_at"); la > 0 {
		row.LastAccessedAt = &la
	}
	row.RealDebridAPIKey = user.RealDebridAPIKey
	return row, nil
}

func (c *Client) DeleteSession(ctx context.Context, token string) error {
	_, err := c.coll("user_sessions").DeleteOne(ctx, bson.M{"session_token": token})
	return err
}

func (c *Client) GetSessionsByUserID(ctx context.Context, userID string) ([]*models.SessionRow, error) {
	cur, err := c.coll("user_sessions").Find(ctx, bson.M{
		"user_id":    userID,
		"expires_at": bson.M{"$gt": nowSec()},
	}, options.Find().SetSort(bson.M{"last_accessed_at": -1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []*models.SessionRow
	for cur.Next(ctx) {
		var sess bson.M
		if err := cur.Decode(&sess); err != nil {
			continue
		}
		s := &models.SessionRow{
			ID:        strVal(sess, "id"),
			UserID:    userID,
			ExpiresAt: int64Val(sess, "expires_at"),
			CreatedAt: int64Val(sess, "created_at"),
		}
		if ua := strVal(sess, "user_agent"); ua != "" {
			s.UserAgent = &ua
		}
		if ip := strVal(sess, "ip_address"); ip != "" {
			s.IPAddress = &ip
		}
		if la := int64Val(sess, "last_accessed_at"); la > 0 {
			s.LastAccessedAt = &la
		}
		out = append(out, s)
	}
	return out, cur.Err()
}

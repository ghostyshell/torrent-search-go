package mongo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func nowSec() int64 {
	return time.Now().Unix()
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userIDFilter(userID string) bson.M {
	if userID == "" {
		return bson.M{"$or": []bson.M{
			{"user_id": nil},
			{"user_id": bson.M{"$exists": false}},
		}}
	}
	return bson.M{"user_id": userID}
}

func userIDsFilter(userIDs []string) bson.M {
	vals := make(bson.A, 0, len(userIDs))
	hasNull := false
	for _, id := range userIDs {
		if id == "" {
			hasNull = true
		} else {
			vals = append(vals, id)
		}
	}
	if hasNull {
		vals = append(vals, nil)
	}
	if len(vals) == 0 {
		return bson.M{"user_id": nil}
	}
	if len(vals) == 1 {
		return bson.M{"user_id": vals[0]}
	}
	return bson.M{"user_id": bson.M{"$in": vals}}
}

var magnetHashRE = regexp.MustCompile(`(?i)xt=urn:btih:([a-fA-F0-9]{40})`)

func extractMagnetHash(magnetLink string) string {
	if m := magnetHashRE.FindStringSubmatch(magnetLink); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return strings.ToLower(magnetLink)
}

func coverDocID(torrentKey string) string {
	return torrentKey + "::cover"
}

func websiteFromTorrentData(raw string) string {
	if raw == "" {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return ""
	}
	if w, ok := data["Website"].(string); ok {
		return w
	}
	if w, ok := data["website"].(string); ok {
		return w
	}
	return ""
}

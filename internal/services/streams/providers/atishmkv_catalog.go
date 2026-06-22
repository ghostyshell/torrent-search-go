package providers

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"torrent-search-go/internal/services/streams"
)

const atishmkvCollection = "atishmkv_catalog"

// AtishmkvCatalog syncs the AtishMKV Marathi catalog to MongoDB.
type AtishmkvCatalog struct {
	client     *mongo.Client
	db         *mongo.Database
	baseURL    string
	category   string
	clientHTTP *streams.HTTPClient
}

// NewAtishmkvCatalog creates a catalog manager. Returns nil if MONGODB_URI is unset.
func NewAtishmkvCatalog(httpClient *streams.HTTPClient) (*AtishmkvCatalog, error) {
	uri := mongoURI()
	if uri == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	dbName := os.Getenv("MONGODB_DB")
	if dbName == "" {
		dbName = "torrent_search"
	}
	cat := &AtishmkvCatalog{
		client:     client,
		db:         client.Database(dbName),
		baseURL:    os.Getenv("ATISHMKV_URL"),
		category:   os.Getenv("ATISHMKV_CATEGORY"),
		clientHTTP: httpClient,
	}
	if cat.baseURL == "" {
		cat.baseURL = "https://atishmkv3.online"
	}
	if cat.category == "" {
		cat.category = "marathi"
	}
	if err := cat.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return cat, nil
}

func mongoURI() string {
	base := os.Getenv("MONGODB_URI")
	if base == "" {
		base = os.Getenv("MONGO_URL")
	}
	if base == "" {
		return ""
	}
	user := os.Getenv("MONGO_USERNAME")
	if user == "" {
		user = os.Getenv("MONGO_USER")
	}
	pass := os.Getenv("MONGO_PASSWORD")
	if pass == "" {
		pass = os.Getenv("MONGO_PASS")
	}
	if user == "" || pass == "" {
		return base
	}
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "mongodb://") && !strings.HasPrefix(lower, "mongodb+srv://") {
		return base
	}
	if strings.Contains(base, "@") {
		return base
	}
	schemeEnd := strings.Index(base, "://")
	if schemeEnd < 0 {
		return base
	}
	return base[:schemeEnd+3] + url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@" + base[schemeEnd+3:]
}

func (c *AtishmkvCatalog) ensureIndexes(ctx context.Context) error {
	col := c.db.Collection(atishmkvCollection)
	_, _ = col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	_, _ = col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "category", Value: 1}, {Key: "year", Value: 1}, {Key: "name", Value: 1}},
	})
	return nil
}

// Close disconnects the catalog MongoDB client.
func (c *AtishmkvCatalog) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.client.Disconnect(ctx)
}

// Sync walks the category pages and upserts detail entries.
func (c *AtishmkvCatalog) Sync(ctx context.Context) (map[string]interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("atishmkv catalog not configured")
	}
	col := c.db.Collection(atishmkvCollection)

	maxPages := 50
	if v := os.Getenv("ATISHMKV_MAX_PAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxPages = n
		}
	}

	detailURLs := make(map[string]string)
	for page := 1; page <= maxPages; page++ {
		var url string
		if page == 1 {
			url = fmt.Sprintf("%s/category/%s/", c.baseURL, c.category)
		} else {
			url = fmt.Sprintf("%s/category/%s/page/%d/", c.baseURL, c.category, page)
		}
		cards, err := c.fetchCards(ctx, url)
		if err != nil || len(cards) == 0 {
			break
		}
		for _, card := range cards {
			detailURLs[card.Link] = card.Title
		}
	}

	upserted, total := 0, 0
	now := time.Now()
	expires := now.Add(7 * 24 * time.Hour)

	for detailURL, cardTitle := range detailURLs {
		entries, err := c.scrapeDetail(ctx, detailURL, cardTitle)
		if err != nil {
			continue
		}
		for _, e := range entries {
			total++
			res, err := col.UpdateOne(ctx,
				bson.M{"_id": e.ID},
				bson.M{"$set": bson.M{
					"source":               e.Source,
					"category":             e.Category,
					"slug":                 e.Slug,
					"detail_url":           e.DetailURL,
					"title":                e.Title,
					"card_title":           e.CardTitle,
					"name":                 e.Name,
					"year":                 e.Year,
					"quality":              e.Quality,
					"size_bytes":           e.SizeBytes,
					"linkoba_url":          e.LinkobaURL,
					"updated_at":           now,
					"expires_at":           expires,
					"last_refresh_attempt": nil,
					"last_refresh_error":   nil,
				}},
				options.Update().SetUpsert(true),
			)
			if err != nil {
				continue
			}
			if res.UpsertedCount > 0 || res.ModifiedCount > 0 {
				upserted++
			}
		}
	}

	return map[string]interface{}{"upserted": upserted, "total": total}, nil
}

func (c *AtishmkvCatalog) fetchCards(ctx context.Context, url string) ([]movieCard, error) {
	data, err := c.clientHTTP.GetText(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	var cards []movieCard
	doc.Find(".movie-card").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find(".movie-title").Text())
		link, _ := s.Find("a").Attr("href")
		if title != "" && link != "" {
			cards = append(cards, movieCard{Title: title, Link: link})
		}
	})
	return cards, nil
}

func (c *AtishmkvCatalog) scrapeDetail(ctx context.Context, detailURL, cardTitle string) ([]atishmkvCatalogEntry, error) {
	data, err := c.clientHTTP.GetText(ctx, detailURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	h1 := strings.TrimSpace(doc.Find("h1").First().Text())
	if h1 == "" {
		h1 = cardTitle
	}
	name, year := parseNameAndYear(h1)
	slug := strings.TrimSuffix(detailURL, "/")
	slug = slug[strings.LastIndex(slug, "/")+1:]

	var entries []atishmkvCatalogEntry
	doc.Find(".download-btn").Each(func(i int, s *goquery.Selection) {
		linkobaURL, _ := s.Attr("href")
		text := strings.TrimSpace(s.Text())
		if linkobaURL == "" || linkobaURL == "#" || text == "" {
			return
		}
		quality := parseAtishmkvQuality(text)
		size := parseAtishmkvSize(text)
		if quality == "" {
			return
		}
		entries = append(entries, atishmkvCatalogEntry{
			ID:         fmt.Sprintf("atishmkv:%s:%s:%s", c.category, slug, quality),
			Source:     "atishmkv",
			Category:   c.category,
			Slug:       slug,
			DetailURL:  detailURL,
			Title:      h1,
			CardTitle:  cardTitle,
			Name:       name,
			Year:       year,
			Quality:    quality,
			SizeBytes:  size,
			LinkobaURL: linkobaURL,
		})
	})
	return entries, nil
}

// Find looks up catalog entries by name/year.
func (c *AtishmkvCatalog) Find(ctx context.Context, name string, year int) ([]atishmkvCatalogEntry, error) {
	if c == nil {
		return nil, nil
	}
	col := c.db.Collection(atishmkvCollection)
	filter := bson.M{"category": c.category}
	if year > 0 {
		filter["year"] = year
	}
	cursor, err := col.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []atishmkvCatalogEntry
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	norm := streams.NormalizeTitle(name)
	var out []atishmkvCatalogEntry
	for _, d := range docs {
		dn := streams.NormalizeTitle(d.Name)
		if strings.Contains(dn, norm) || strings.Contains(norm, dn) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Stats returns catalog counts.
func (c *AtishmkvCatalog) Stats(ctx context.Context) (map[string]interface{}, error) {
	if c == nil {
		return map[string]interface{}{"enabled": false}, nil
	}
	col := c.db.Collection(atishmkvCollection)
	total, _ := col.CountDocuments(ctx, bson.M{"category": c.category})
	return map[string]interface{}{"total": total, "enabled": true}, nil
}

type movieCard struct {
	Title string
	Link  string
}

type atishmkvCatalogEntry struct {
	ID         string `bson:"_id"`
	Source     string `bson:"source"`
	Category   string `bson:"category"`
	Slug       string `bson:"slug"`
	DetailURL  string `bson:"detail_url"`
	Title      string `bson:"title"`
	CardTitle  string `bson:"card_title"`
	Name       string `bson:"name"`
	Year       int    `bson:"year"`
	Quality    string `bson:"quality"`
	SizeBytes  int64  `bson:"size_bytes"`
	LinkobaURL string `bson:"linkoba_url"`
}

func parseNameAndYear(text string) (string, int) {
	m := regexp.MustCompile(`^(.+?)\s*\((\d{4})\)`).FindStringSubmatch(text)
	if len(m) == 3 {
		year, _ := strconv.Atoi(m[2])
		return strings.TrimSpace(m[1]), year
	}
	parts := strings.Split(text, "|")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0]), 0
	}
	return strings.TrimSpace(text), 0
}

func parseAtishmkvQuality(text string) string {
	m := regexp.MustCompile(`(?i)\b(480p|720p|1080p|2160p|4k)\b`).FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	q := strings.ToLower(m[1])
	if q == "4k" {
		q = "2160p"
	}
	rip := regexp.MustCompile(`(?i)\b(webrip|web-dl|hdtc|hdts|bdrip|brrip|bluray|dvdrip)\b`).FindStringSubmatch(text)
	if len(rip) > 1 {
		return q + " " + strings.ToUpper(rip[1])
	}
	return q
}

func parseAtishmkvSize(text string) int64 {
	m := regexp.MustCompile(`(?i)([\d.]+)\s*(KB|MB|GB|TB)`).FindStringSubmatch(text)
	if len(m) < 3 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	units := map[string]int64{"kb": 1024, "mb": 1024 * 1024, "gb": 1024 * 1024 * 1024, "tb": 1024 * 1024 * 1024 * 1024}
	return int64(v * float64(units[strings.ToLower(m[2])]))
}

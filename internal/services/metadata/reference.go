package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pornripsWPBase = "https://pornrips.to"

// wpPost is the subset of a WordPress REST API post we need.
type wpPost struct {
	ID         int    `json:"id"`
	Slug       string `json:"slug"`
	Date       string `json:"date"`
	Tags       []int  `json:"tags"`
	Categories []int  `json:"categories"`
	Title      struct{ Rendered string `json:"rendered"` } `json:"title"`
	Excerpt    struct{ Rendered string `json:"rendered"` } `json:"excerpt"`
	Embedded   struct {
		FeaturedMedia []struct {
			SourceURL string `json:"source_url"`
		} `json:"wp:featuredmedia"`
		// Terms is the _embed=wp:term result: one slice per taxonomy. PornRips
		// uses the post_tag taxonomy for the release's studio/site (e.g.
		// "BrazzersExxtra"); categories are only quality buckets (720p/1080p),
		// so the studio is read from the post_tag term, not categories.
		Terms [][]wpTerm `json:"wp:term"`
	} `json:"_embedded"`
}

// wpTerm is a WordPress category or tag.
type wpTerm struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Taxonomy string `json:"taxonomy"`
}

// ReferenceMeta is normalized metadata from the PornRips WordPress API.
type ReferenceMeta struct {
	Name        string   `json:"name"`
	Poster      string   `json:"poster"`
	Background  string   `json:"background"`
	Description string   `json:"description"`
	Year        string   `json:"year"`
	Runtime     string   `json:"runtime,omitempty"`
	Studio      string   `json:"studio,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Cast        []string `json:"cast,omitempty"`
}

// ReferenceClient fetches PornRips metadata from the pornrips.to WordPress REST API.
type ReferenceClient struct {
	baseURL    string
	httpClient *http.Client
	termCache  sync.Map // "categories:Name" or "tags:Name" → int ID
}

// NewReferenceClient creates a client backed by the pornrips.to WordPress API.
func NewReferenceClient() *ReferenceClient {
	return &ReferenceClient{
		baseURL:    pornripsWPBase,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled always returns true - the WP source is always available.
func (c *ReferenceClient) Enabled() bool { return true }

// GetPornripsMeta fetches metadata for a single PornRips slug via the WP API.
func (c *ReferenceClient) GetPornripsMeta(ctx context.Context, slug string) (*ReferenceMeta, error) {
	if slug == "" {
		return nil, nil
	}
	posts, err := c.fetchPosts(ctx, url.Values{
		"slug":   {slug},
		"_embed": {"wp:featuredmedia,wp:term"},
	})
	if err != nil || len(posts) == 0 {
		return nil, err
	}
	return wpPostToMeta(posts[0]), nil
}

// FetchRecent returns recently published PornRips items from the WP API.
func (c *ReferenceClient) FetchRecent(ctx context.Context, skip int) ([]ReferenceRecentItem, error) {
	const perPage = 24
	page := skip/perPage + 1
	posts, err := c.fetchPosts(ctx, url.Values{
		"per_page": {strconv.Itoa(perPage)},
		"page":     {strconv.Itoa(page)},
		"_embed":   {"wp:featuredmedia,wp:term"},
	})
	if err != nil {
		return nil, err
	}
	return postsToItems(posts), nil
}

// FetchPornripsCatalog returns catalog items filtered by studio, tag, or search.
func (c *ReferenceClient) FetchPornripsCatalog(ctx context.Context, refCat, value string, skip int) ([]ReferenceRecentItem, error) {
	const perPage = 24
	page := skip/perPage + 1
	params := url.Values{
		"per_page": {strconv.Itoa(perPage)},
		"page":     {strconv.Itoa(page)},
		"_embed":   {"wp:featuredmedia,wp:term"},
	}
	switch refCat {
	case "search":
		if value != "" {
			params.Set("search", value)
		}
	case "studio":
		// If the studio name doesn't resolve to a WP category ID, return
		// nothing: fetching posts without the categories filter would return
		// the recent feed, which the caller treats as a successful filtered
		// result and never falls through to the token-scan / WP ?s= paths
		// that actually filter by the studio name.
		id, _ := c.resolveTermID(ctx, "categories", value)
		if id <= 0 {
			return nil, nil
		}
		params.Set("categories", strconv.Itoa(id))
	case "tag":
		id, _ := c.resolveTermID(ctx, "tags", value)
		if id <= 0 {
			return nil, nil
		}
		params.Set("tags", strconv.Itoa(id))
	}
	posts, err := c.fetchPosts(ctx, params)
	if err != nil {
		return nil, err
	}
	return postsToItems(posts), nil
}

// ReferenceRecentItem is one entry from a PornRips catalog page.
type ReferenceRecentItem struct {
	Slug string
	Date string // full WP post date (YYYY-MM-DDThh:mm:ss), for newest-first ordering in the Mongo store
	Meta *ReferenceMeta
}

func (c *ReferenceClient) resolveTermID(ctx context.Context, taxonomy, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	cacheKey := taxonomy + ":" + name
	if v, ok := c.termCache.Load(cacheKey); ok {
		return v.(int), nil
	}
	reqURL := fmt.Sprintf("%s/wp-json/wp/v2/%s?search=%s&per_page=10",
		c.baseURL, taxonomy, url.QueryEscape(name))
	body, err := c.getJSON(ctx, reqURL)
	if err != nil {
		return 0, err
	}
	var terms []wpTerm
	if err := json.Unmarshal(body, &terms); err != nil {
		return 0, err
	}
	for _, t := range terms {
		if strings.EqualFold(t.Name, name) || strings.EqualFold(t.Slug, strings.ReplaceAll(strings.ToLower(name), " ", "-")) {
			c.termCache.Store(cacheKey, t.ID)
			return t.ID, nil
		}
	}
	// Cache the miss so paginating a studio/tag whose name doesn't match a WP
	// term (common: "5K Porn", "BANG!", "ATKingdom") doesn't re-query the term
	// search endpoint on every skip page.
	c.termCache.Store(cacheKey, 0)
	return 0, nil
}

func (c *ReferenceClient) fetchPosts(ctx context.Context, params url.Values) ([]wpPost, error) {
	reqURL := c.baseURL + "/wp-json/wp/v2/posts?" + params.Encode()
	body, err := c.getJSON(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	var posts []wpPost
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

func (c *ReferenceClient) getJSON(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	// WP REST API returns 400 when page exceeds total pages - treat as empty.
	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusNotFound {
		return []byte("[]"), nil
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("pornrips WP API %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTagRE.ReplaceAllString(s, "")))
}

func wpPostToMeta(p wpPost) *ReferenceMeta {
	title := html.UnescapeString(p.Title.Rendered)
	if title == "" && p.Slug == "" {
		return nil
	}
	poster := ""
	if len(p.Embedded.FeaturedMedia) > 0 {
		poster = p.Embedded.FeaturedMedia[0].SourceURL
	}
	year := ""
	if len(p.Date) >= 4 {
		year = p.Date[:4]
	}
	return &ReferenceMeta{
		Name:        title,
		Poster:      poster,
		Background:  poster,
		Description: stripHTML(p.Excerpt.Rendered),
		Year:        year,
		Studio:      postTagStudio(p),
	}
}

// postTagStudio returns the release's studio/site from the post's post_tag term.
// PornRips tags each post with exactly one site name (e.g. "BrazzersExxtra"),
// which is the authoritative studio - more reliable than parsing the release
// filename and available for every post without a TPDB lookup.
func postTagStudio(p wpPost) string {
	for _, group := range p.Embedded.Terms {
		for _, t := range group {
			if strings.EqualFold(t.Taxonomy, "post_tag") && t.Name != "" {
				return t.Name
			}
		}
	}
	return ""
}

func postsToItems(posts []wpPost) []ReferenceRecentItem {
	out := make([]ReferenceRecentItem, 0, len(posts))
	for _, p := range posts {
		if p.Slug == "" {
			continue
		}
		out = append(out, ReferenceRecentItem{Slug: p.Slug, Date: p.Date, Meta: wpPostToMeta(p)})
	}
	return out
}

// ToSharedMeta maps reference metadata into the TPDB-shared store shape.
func (m *ReferenceMeta) ToSharedMeta() SharedMetaFromReference {
	if m == nil {
		return SharedMetaFromReference{}
	}
	return SharedMetaFromReference{
		Title:       m.Name,
		Poster:      m.Poster,
		Background:  m.Background,
		Description: m.Description,
		Year:        m.Year,
		Cast:        m.Cast,
		Genres:      m.Genres,
		Source:      "reference",
	}
}

// SharedMetaFromReference is the reference-warmer write shape.
type SharedMetaFromReference struct {
	Title       string   `json:"title"`
	Poster      string   `json:"poster"`
	Background  string   `json:"background"`
	Description string   `json:"description"`
	Year        string   `json:"year"`
	Cast        []string `json:"cast"`
	Genres      []string `json:"genres"`
	Source      string   `json:"source"`
}


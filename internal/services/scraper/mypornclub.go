package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
)

const mypornclubBaseURL = "https://myporn.club"

// mypornclubMax bounds the per-page detail fetches: the listing / livesearch
// response carries no magnet, so each result needs a /t/<alpha_id> detail fetch.
// ponytail: cap at 12 with full concurrency; raise if recall matters, but every
// result is an extra HTTP request against the site.
const mypornclubMax = 12

var mypornclubTagRE = regexp.MustCompile(`<[^>]*>`)

type MypornClubScraper struct {
	client  *http.Client
	baseURL string
}

func NewMypornClubScraper(client *http.Client) *MypornClubScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &MypornClubScraper{client: client, baseURL: mypornclubBaseURL}
}

// Search hits the livesearch API (POST /api/v0.1/livesearch) which returns
// alpha_ids + descriptive text, then resolves magnets from each detail page.
func (s *MypornClubScraper) Search(ctx context.Context, query string, page int, _ models.SearchOptions) ([]models.Torrent, error) {
	items, err := s.livesearch(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.resolveDetails(ctx, items), nil
}

// Browse parses the server-rendered homepage listing (latest + top hits) for
// alpha_ids + titles + size + seeders, then resolves magnets from detail pages.
func (s *MypornClubScraper) Browse(ctx context.Context, category string, page int, _ string, _ models.SearchOptions) ([]models.Torrent, error) {
	items, err := s.listIDs(ctx)
	if err != nil {
		return nil, err
	}
	return s.resolveDetails(ctx, items), nil
}

type mypornItem struct {
	alphaID string
	title   string
}

// livesearch calls the POST livesearch endpoint and returns alpha_id + cleaned title.
func (s *MypornClubScraper) livesearch(ctx context.Context, query string) ([]mypornItem, error) {
	form := url.Values{}
	form.Set("key", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v0.1/livesearch", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, &ScraperError{Message: "mypornclub livesearch request failed", Err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "mypornclub livesearch fetch failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("mypornclub livesearch returned %d", resp.StatusCode)}
	}

	var payload struct {
		Searches []struct{ Skey string `json:"skey"` } `json:"searches"`
		Torrents []struct {
			AlphaID string `json:"alpha_id"`
			Text    string `json:"text"`
		} `json:"torrents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, &ScraperError{Message: "mypornclub livesearch parse failed", Err: err}
	}

	out := make([]mypornItem, 0, mypornclubMax)
	for _, t := range payload.Torrents {
		if t.AlphaID == "" {
			continue
		}
		out = append(out, mypornItem{alphaID: t.AlphaID, title: cleanMypornTitle(t.Text)})
		if len(out) >= mypornclubMax {
			break
		}
	}
	return out, nil
}

// listIDs scrapes the homepage for torrent alpha_ids + titles + size + seeders.
func (s *MypornClubScraper) listIDs(ctx context.Context) ([]mypornItem, error) {
	doc, err := s.fetchDoc(ctx, "/")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]mypornItem, 0, mypornclubMax)
	doc.Find("a[href^='/t/']").Each(func(_ int, a *goquery.Selection) {
		if len(out) >= mypornclubMax {
			return
		}
		id := strings.TrimPrefix(a.AttrOr("href", ""), "/t/")
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, mypornItem{alphaID: id, title: cleanMypornTitle(a.AttrOr("title", ""))})
	})
	return out, nil
}

// resolveDetails fetches each torrent's /t/<alpha_id> detail page concurrently
// for the magnet (the listing carries none), then parses size/seeders/leechers.
func (s *MypornClubScraper) resolveDetails(ctx context.Context, items []mypornItem) []models.Torrent {
	if len(items) == 0 {
		return nil
	}
	out := make([]models.Torrent, 0, len(items))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, mypornclubMax)
	for _, it := range items {
		it := it
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t, ok := s.fetchDetail(ctx, it)
			if !ok {
				return
			}
			mu.Lock()
			out = append(out, t)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func (s *MypornClubScraper) fetchDetail(ctx context.Context, it mypornItem) (models.Torrent, bool) {
	cctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	doc, err := s.fetchDoc(cctx, "/t/"+it.alphaID)
	if err != nil {
		return models.Torrent{}, false
	}
	magnet, _ := doc.Find("a[href^='magnet:']").First().Attr("href")
	if magnet == "" {
		return models.Torrent{}, false
	}
	size := strings.TrimSpace(doc.Find(".tsize_span").First().Text())
	seeders := atoiSafe(doc.Find(".teiv_seeders").First().Text())
	leechers := atoiSafe(doc.Find(".teiv_leechers").First().Text())
	title := it.title
	if title == "" {
		// Fall back to the magnet display name (dotted release filename).
		title = decodeMagnetName(magnet)
	}
	return models.Torrent{
		Name:       title,
		MagnetLink: magnet,
		Seeders:    seeders,
		Leechers:   leechers,
		Size:       size,
		Website:    "mypornclub",
		TorrentURL: s.baseURL + "/t/" + it.alphaID,
	}, true
}

func (s *MypornClubScraper) fetchDoc(ctx context.Context, path string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, &ScraperError{Message: "mypornclub request failed", Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "mypornclub fetch failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("mypornclub returned %d", resp.StatusCode)}
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "mypornclub parse failed", Err: err}
	}
	return doc, nil
}

// cleanMypornTitle strips HTML tags from a livesearch/text blob and collapses
// whitespace. The listing titles carry inline #tags and stream URLs; the metadata
// parser (ParseRelease) is robust to that noise, so no further grooming here.
func cleanMypornTitle(s string) string {
	s = mypornclubTagRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#039;", "'")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

// decodeMagnetName pulls the &dn= display name off a magnet URI (URL-decoded).
func decodeMagnetName(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	if dn := u.Query().Get("dn"); dn != "" {
		return dn
	}
	return ""
}

var (
	_ Scraper       = (*MypornClubScraper)(nil)
	_ BrowseScraper = (*MypornClubScraper)(nil)
)
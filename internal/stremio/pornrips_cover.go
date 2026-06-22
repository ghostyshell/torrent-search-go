package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	prefixPornripsCover = "prcover:v1:"
	ttlPornripsCover    = 7 * 24 * time.Hour
	ttlPornripsCoverNeg = 6 * time.Hour
	pornripsCoverClient = 6 * time.Second
)

var pornripsCoverHTTP = &http.Client{Timeout: pornripsCoverClient}

type pornripsCoverCache struct {
	Found bool   `json:"found"`
	URL   string `json:"url,omitempty"`
}

// fetchPornripsDetailCover returns the og:image cover from a PornRips post page.
func (h *Handler) fetchPornripsDetailCover(ctx context.Context, detailURL string) string {
	slug := PornripsSlug(detailURL)
	if slug == "" {
		return ""
	}
	store := newRedisStore(h.Redis)
	if store != nil {
		if cover, found := store.getPornripsCover(ctx, slug); found {
			return cover
		}
	}

	rctx, cancel := context.WithTimeout(ctx, pornripsCoverClient)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, detailURL, nil)
	if err != nil {
		h.cachePornripsCover(ctx, store, slug, "")
		return ""
	}
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "+
			"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := pornripsCoverHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		h.cachePornripsCover(ctx, store, slug, "")
		return ""
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		h.cachePornripsCover(ctx, store, slug, "")
		return ""
	}
	cover := ""
	for _, sel := range []string{`meta[property="og:image"]`, `meta[name="twitter:image"]`} {
		if c, ok := doc.Find(sel).First().Attr("content"); ok && strings.TrimSpace(c) != "" {
			cover = strings.TrimSpace(c)
			break
		}
	}
	h.cachePornripsCover(ctx, store, slug, cover)
	return cover
}

func (h *Handler) cachePornripsCover(ctx context.Context, store *redisStore, slug, cover string) {
	if store == nil || slug == "" {
		return
	}
	entry := pornripsCoverCache{Found: true, URL: cover}
	ttl := ttlPornripsCover
	if cover == "" {
		ttl = ttlPornripsCoverNeg
	}
	_ = store.setPornripsCover(ctx, slug, entry, ttl)
}

func (h *Handler) enrichPornripsDetailCovers(ctx context.Context, torrents []catalogTorrent, metas []MetaPreview) {
	need := make([]int, 0)
	for i, m := range metas {
		if torrents[i].Website != "pornrips" {
			continue
		}
		if m.Poster != "" && !strings.HasPrefix(m.Poster, "data:image/svg") {
			continue
		}
		if strings.TrimSpace(torrents[i].DetailURL) == "" {
			continue
		}
		need = append(need, i)
	}
	if len(need) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, catalogLiveMetaTimeout)
	defer cancel()

	type result struct {
		idx   int
		cover string
	}
	ch := make(chan result, len(need))
	sem := make(chan struct{}, refCatalogConcurrency)
	var wg sync.WaitGroup
	for _, idx := range need {
		idx := idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			cover := h.fetchPornripsDetailCover(ctx, torrents[idx].DetailURL)
			if cover != "" {
				ch <- result{idx: idx, cover: cover}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	for r := range ch {
		metas[r.idx].Poster = r.cover
		metas[r.idx].Background = r.cover
	}
}

func (s *redisStore) getPornripsCover(ctx context.Context, slug string) (string, bool) {
	if s == nil || s.client == nil || slug == "" {
		return "", false
	}
	raw, ok, err := s.client.Get(ctx, prefixPornripsCover+slug)
	if err != nil || !ok {
		return "", false
	}
	var entry pornripsCoverCache
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return "", false
	}
	return entry.URL, true
}

func (s *redisStore) setPornripsCover(ctx context.Context, slug string, entry pornripsCoverCache, ttl time.Duration) error {
	if s == nil || s.client == nil || slug == "" {
		return nil
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, prefixPornripsCover+slug, string(b), ttl)
}

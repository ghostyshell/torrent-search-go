package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/services/streams"
)

// AtishmkvResolver resolves linkoba URLs to final direct video URLs.
type AtishmkvResolver struct {
	client    *streams.HTTPClient
	preferred []string
}

// NewAtishmkvResolver creates a resolver.
func NewAtishmkvResolver(client *streams.HTTPClient) *AtishmkvResolver {
	prefs := []string{"Hubcloud", "Gdflix", "Hubdrive", "Filepress"}
	if v := "Hubcloud,Gdflix,Hubdrive,Filepress"; v != "" {
		// TODO: read from env
	}
	return &AtishmkvResolver{client: client, preferred: prefs}
}

// ResolveDirect walks the redirect chain for a linkoba URL.
func (r *AtishmkvResolver) ResolveDirect(ctx context.Context, linkobaURL string) (string, string, error) {
	hosts, err := r.fetchHostLandingURLs(ctx, linkobaURL)
	if err != nil || len(hosts) == 0 {
		return "", "", fmt.Errorf("no host landing URLs")
	}
	ordered := r.orderHosts(hosts)
	for _, h := range ordered {
		url, err := r.resolveHost(ctx, h.Host, h.URL)
		if err == nil && url != "" {
			return url, h.Host, nil
		}
	}
	return "", "", fmt.Errorf("all hosts failed")
}

type hostLanding struct {
	Host string
	URL  string
}

func (r *AtishmkvResolver) fetchHostLandingURLs(ctx context.Context, linkobaURL string) ([]hostLanding, error) {
	data, err := r.client.GetText(ctx, linkobaURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	var hosts []hostLanding
	doc.Find(".linkoba-btn[data-url]").Each(func(i int, s *goquery.Selection) {
		u, _ := s.Attr("data-url")
		host, _ := s.Attr("data-host")
		if u != "" && host != "" {
			hosts = append(hosts, hostLanding{Host: strings.TrimSpace(host), URL: u})
		}
	})
	return hosts, nil
}

func (r *AtishmkvResolver) orderHosts(hosts []hostLanding) []hostLanding {
	score := func(h string) int {
		for i, p := range r.preferred {
			if strings.EqualFold(h, p) {
				return i
			}
		}
		return 1000
	}
	out := make([]hostLanding, len(hosts))
	copy(out, hosts)
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if score(out[i].Host) > score(out[j].Host) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (r *AtishmkvResolver) resolveHost(ctx context.Context, host, landingURL string) (string, error) {
	switch strings.ToLower(host) {
	case "hubcloud":
		return r.resolveHubcloud(ctx, landingURL)
	default:
		return r.resolveGeneric(ctx, landingURL)
	}
}

func (r *AtishmkvResolver) resolveHubcloud(ctx context.Context, landingURL string) (string, error) {
	data, err := r.client.GetText(ctx, landingURL)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	genURL := ""
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if genURL != "" {
			return
		}
		href, _ := s.Attr("href")
		if strings.Contains(href, "hubcloud.php") || strings.Contains(href, "gamerxyt.com") {
			genURL = href
		}
	})
	if genURL == "" {
		return "", fmt.Errorf("hubcloud generator not found")
	}

	data, err = r.client.GetText(ctx, genURL)
	if err != nil {
		return "", err
	}
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	mirror := ""
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if mirror != "" {
			return
		}
		href, _ := s.Attr("href")
		if strings.Contains(href, "gpdl2.hubcloud.cx") {
			mirror = href
		}
	})
	if mirror == "" {
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			if mirror != "" {
				return
			}
			href, _ := s.Attr("href")
			if strings.Contains(href, "hubcloud.cx") {
				mirror = href
			}
		})
	}
	if mirror == "" {
		return "", fmt.Errorf("hubcloud mirror not found")
	}

	final, err := r.client.FollowRedirect(ctx, mirror)
	if err != nil {
		return "", err
	}
	return extractDirectFromDlPhp(final)
}

func (r *AtishmkvResolver) resolveGeneric(ctx context.Context, landingURL string) (string, error) {
	data, err := r.client.GetText(ctx, landingURL)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	var link string
	doc.Find("a[download]").Each(func(i int, s *goquery.Selection) {
		if link != "" {
			return
		}
		href, _ := s.Attr("href")
		if href != "" {
			link = href
		}
	})
	if link == "" {
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			if link != "" {
				return
			}
			href, _ := s.Attr("href")
			if strings.Contains(href, "dl.php") {
				link = href
			}
		})
	}
	if link == "" {
		return "", fmt.Errorf("generic download link not found")
	}
	if !strings.HasPrefix(link, "http") {
		base, _ := url.Parse(landingURL)
		ref, _ := base.Parse(link)
		link = ref.String()
	}
	final, err := r.client.FollowRedirect(ctx, link)
	if err != nil {
		return link, nil
	}
	if u, err := extractDirectFromDlPhp(final); err == nil && u != "" {
		return u, nil
	}
	return final, nil
}

func extractDirectFromDlPhp(redirect string) (string, error) {
	u, err := url.Parse(redirect)
	if err == nil {
		link := u.Query().Get("link")
		if link != "" {
			return link, nil
		}
	}
	m := regexp.MustCompile(`https?://[^\s"<>]+`).FindString(redirect)
	if m != "" {
		return m, nil
	}
	return "", fmt.Errorf("direct URL not found")
}

// VerifyDirect does a HEAD check that the URL is a video.
func (r *AtishmkvResolver) VerifyDirect(ctx context.Context, url string) bool {
	if r.client == nil {
		return false
	}
	c := r.client.Client
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "video/") || strings.Contains(ct, "octet-stream") || strings.Contains(ct, "mkv")
}

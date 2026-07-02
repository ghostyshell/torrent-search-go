package extractors

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type fastpicExtractor struct{}

var (
	fastpicSignedRE = regexp.MustCompile(`https?://i\d+\.fastpic\.org/[^"'\s]+\.(jpg|jpeg|png|gif|webp)\?md5=[^"'\s]+&expires=\d+`)
	fastpicViewRE   = regexp.MustCompile(`https?://fastpic\.[^/]+/view/(\d+)/(\d{4})/(\d{4})/_([a-f0-9]+)\.(\w+)\.html`)
)

func (e *fastpicExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	if isDirectImageURL(imagePageURL) && !strings.HasSuffix(imagePageURL, ".html") {
		return imagePageURL, nil
	}

	u, err := url.Parse(imagePageURL)
	if err != nil {
		return "", err
	}
	if !strings.Contains(u.Path, "/view/") || !strings.HasSuffix(u.Path, ".html") {
		return imagePageURL, nil
	}

	body, err := fetchBody(ctx, client, imagePageURL)
	if err == nil && len(body) > 0 {
		doc, docErr := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if docErr == nil {
			if meta := metaImage(doc); meta != "" {
				if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
					return normalized, nil
				}
			}

			selectors := []string{
				"img#image",
				"img.main-image",
				"img[src*=\"fastpic.org\"]",
				".image-container img",
				"#main-image",
				"img.img-fluid",
				"img[onclick*=\"fullview\"]",
				"a[href*=\"fullview\"] img",
				"img[src*=\"?md5=\"]",
			}
			for _, sel := range selectors {
				if src, ok := doc.Find(sel).First().Attr("src"); ok {
					if normalized := normalizeURL(src, imagePageURL); isDirectImageURL(normalized) {
						return normalized, nil
					}
				}
			}

			for _, script := range doc.Find("script").Nodes {
				if script.FirstChild != nil {
					if m := fastpicSignedRE.FindString(script.FirstChild.Data); m != "" {
						return m, nil
					}
				}
			}

			if href, ok := doc.Find("a[href*=\"fullview\"]").First().Attr("href"); ok {
				return e.Extract(ctx, client, normalizeURL(href, imagePageURL))
			}
		}

		if m := fastpicSignedRE.Find(body); m != nil {
			return string(m), nil
		}
	}

	m := fastpicViewRE.FindStringSubmatch(imagePageURL)
	if m == nil {
		return imagePageURL, nil
	}
	server, year, monthDay, hash, ext := m[1], m[2], m[3], m[4], m[5]

	candidates := []string{
		fmt.Sprintf("https://i%s.fastpic.org/big/%s/%s/%s/%s.%s", server, year, monthDay, hash[:2], hash, ext),
		fmt.Sprintf("https://i%s.fastpic.org/%s/%s/%s.%s", server, year, monthDay, hash, ext),
		fmt.Sprintf("https://fastpic.org/big/%s/%s/%s.%s", year, monthDay, hash, ext),
	}

	for _, candidate := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, candidate, nil)
		if err != nil {
			continue
		}
		setCommonHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return candidate, nil
		}
	}

	return candidates[0], nil
}

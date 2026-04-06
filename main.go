// XWorld Backend — Go port of xserver.js
// Run: go mod tidy && go run main.go
package main

import (
	"context" // Add this to your imports at the top
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

const PORT = 1234

// ─── Site config ─────────────────────────────────────────────────────────────

type siteSection struct {
	Slug string
	Name string
}

type siteConfig struct {
	Base     string
	Headers  map[string]string
	Cookies  map[string]string
	Sections []siteSection
}

var sites = map[string]siteConfig{
	"xhamster": {
		Base: "https://xhamster.com",
		Headers: map[string]string{
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124 Safari/537.36",
			"Accept-Language": "en-US,en;q=0.9",
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Referer":         "https://xhamster.com/",
		},
		Cookies: map[string]string{"video_titles_translation": "0", "geo": "us"},
		Sections: []siteSection{
			{"/4k", "4K"}, {"/hd", "1080p"}, {"/categories/teen", "Teen"},
			{"/categories/milf", "MILF"}, {"/categories/mature", "Mature"}, {"/categories/amateur", "Amateur"},
			{"/categories/big-ass", "Big Ass"}, {"/categories/anal", "Anal"}, {"/categories/hardcore", "Hardcore"},
			{"/categories/homemade", "Homemade"}, {"/categories/lesbian", "Lesbian"}, {"/categories/asian", "Asian"},
		},
	},
	"xvideos": {
		Base: "https://www.xvideos.com",
		Headers: map[string]string{
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124 Safari/537.36",
			"Accept-Language": "en-US,en;q=0.9",
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Referer":         "https://www.xvideos.com/",
		},
		Cookies: map[string]string{},
		Sections: []siteSection{
			{"", "Featured"}, {"/c/Amateur-65", "Amateur"}, {"/c/Anal-12", "Anal"},
			{"/c/Big_Tits-23", "Big Tits"}, {"/c/Big_Ass-24", "Big Ass"}, {"/c/Milf-19", "MILF"},
			{"/c/Mature-38", "Mature"}, {"/c/Teen-13", "Teen"}, {"/c/Lesbian-26", "Lesbian"},
			{"/c/Blowjob-15", "Blowjob"}, {"/c/Creampie-40", "Creampie"},
		},
	},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func init() {
	// Force the Go resolver to use Google's Public DNS
	// This bypasses the [::1]:53 "Connection Refused" error on Android
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 5,
			}
			return d.DialContext(ctx, "udp", "8.8.8.8:53")
		},
	}
}

// ─── Fetch ────────────────────────────────────────────────────────────────────

func fetchPage(pageURL, site string) (*goquery.Document, string, error) {
	cfg := sites[site]
	cookies := make([]string, 0, len(cfg.Cookies))
	for k, v := range cfg.Cookies {
		cookies = append(cookies, k+"="+v)
	}

	var lastErr error
	for i := 0; i <= 2; i++ {
		req, err := http.NewRequest("GET", pageURL, nil)
		if err != nil {
			return nil, "", err
		}
		for k, v := range cfg.Headers {
			req.Header.Set(k, v)
		}
		if len(cookies) > 0 {
			req.Header.Set("Cookie", strings.Join(cookies, "; "))
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(900 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}
		html := string(b)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, "", err
		}
		return doc, html, nil
	}
	return nil, "", fmt.Errorf("fetch failed [%s]: %v", site, lastErr)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func absURL(href, base string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "/") {
		return base + href
	}
	return base + "/" + href
}

// extractJsonObject does brace-counting JSON extraction from raw HTML
func extractJsonObject(html, varName string) map[string]interface{} {
	marker := varName + "="
	start := strings.Index(html, marker)
	if start == -1 {
		return nil
	}
	bStart := strings.Index(html[start+len(marker):], "{")
	if bStart == -1 {
		return nil
	}
	bStart += start + len(marker)

	depth := 0
	inStr := false
	esc := false
	strCh := rune(0)

	for i, ch := range html[bStart:] {
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if !inStr && (ch == '"' || ch == '\'') {
			inStr = true
			strCh = ch
			continue
		}
		if inStr && ch == strCh {
			inStr = false
			continue
		}
		if inStr {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				chunk := html[bStart : bStart+i+1]
				var out map[string]interface{}
				if err := json.Unmarshal([]byte(chunk), &out); err != nil {
					return nil
				}
				return out
			}
		}
	}
	return nil
}

// ─── xHamster ────────────────────────────────────────────────────────────────

type VideoCard struct {
	Title    string `json:"title"`
	Href     string `json:"href"`
	Poster   string `json:"poster"`
	Duration string `json:"duration"`
}

func parseXhCards(doc *goquery.Document, base string) []VideoCard {
	var items []VideoCard
	doc.Find("div.thumb-list__item, .thumb-list .thumb").Each(func(_ int, el *goquery.Selection) {
		a := el.Find("a.video-thumb-info__name").First()
		title := strings.TrimSpace(a.Text())
		href := a.AttrOr("href", "")
		img := el.Find("img.thumb-image-container__image").First()
		poster := strings.TrimSpace(img.AttrOr("data-src", img.AttrOr("src", "")))
		dur := strings.TrimSpace(el.Find(".thumb-image-container__duration").Text())
		if title != "" && href != "" {
			items = append(items, VideoCard{Title: title, Href: absURL(href, base), Poster: poster, Duration: dur})
		}
	})
	return items
}

type SectionResult struct {
	Name  string      `json:"name"`
	Items interface{} `json:"items"`
	Error string      `json:"error,omitempty"`
}

func xhHome(page int) map[string]interface{} {
	cfg := sites["xhamster"]
	secs := cfg.Sections
	if len(secs) > 6 {
		secs = secs[:6]
	}
	results := make([]SectionResult, len(secs))
	done := make(chan int, len(secs))
	for i, s := range secs {
		go func(i int, slug, name string) {
			pageURL := fmt.Sprintf("%s%s/%d?geo=us", cfg.Base, slug, page)
			doc, _, err := fetchPage(pageURL, "xhamster")
			if err != nil {
				results[i] = SectionResult{Name: name, Items: []VideoCard{}, Error: err.Error()}
			} else {
				results[i] = SectionResult{Name: name, Items: parseXhCards(doc, cfg.Base)}
			}
			done <- i
		}(i, s.Slug, s.Name)
	}
	for range secs {
		<-done
	}
	return map[string]interface{}{"sections": results}
}

func xhSearch(query string, page int) (map[string]interface{}, error) {
	cfg := sites["xhamster"]
	q := strings.ReplaceAll(query, " ", "+")
	u := fmt.Sprintf("%s/search/%s/?page=%d&x_platform_switch=desktop&geo=us", cfg.Base, q, page)
	doc, _, err := fetchPage(u, "xhamster")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"items": parseXhCards(doc, cfg.Base)}, nil
}

func xhDetail(pageURL string) (map[string]interface{}, error) {
	cleanURL := pageURL
	if strings.Contains(pageURL, "?") {
		cleanURL += "&geo=us"
	} else {
		cleanURL += "?geo=us"
	}
	doc, _, err := fetchPage(cleanURL, "xhamster")
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("div.with-player-container h1, h1.page-title").First().Text())
	if title == "" {
		title = doc.Find("meta[property='og:title']").AttrOr("content", "Unknown")
	}

	poster := doc.Find("meta[property='og:image']").AttrOr("content", "")
	if poster == "" {
		style := doc.Find("div.xp-preload-image, .preload-image").First().AttrOr("style", "")
		if m := regexp.MustCompile(`url\(['"']?(https?[^'")\s]+)['"']?\)`).FindStringSubmatch(style); len(m) > 1 {
			poster = m[1]
		}
	}

	var tags []string
	doc.Find("a.video-tag, nav#video-tags-list-container a").Each(func(_ int, s *goquery.Selection) {
		if t := strings.TrimSpace(s.Text()); t != "" {
			tags = append(tags, t)
		}
	})

	var recs []VideoCard
	doc.Find("div.related-container div.thumb-list__item, .related-container .thumb").Each(func(_ int, el *goquery.Selection) {
		a := el.Find("a.video-thumb-info__name")
		t := strings.TrimSpace(a.Text())
		h := a.AttrOr("href", "")
		img := el.Find("img")
		p := img.AttrOr("data-src", img.AttrOr("src", ""))
		if t != "" && h != "" {
			recs = append(recs, VideoCard{Title: t, Href: absURL(h, sites["xhamster"].Base), Poster: p})
		}
	})
	if tags == nil {
		tags = []string{}
	}
	if recs == nil {
		recs = []VideoCard{}
	}
	return map[string]interface{}{"title": title, "poster": poster, "tags": tags, "recs": recs, "pageUrl": pageURL}, nil
}

type StreamLink struct {
	Src   string `json:"src"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

func xhLinks(pageURL string) (map[string]interface{}, error) {
	cleanURL := pageURL
	if strings.Contains(pageURL, "?") {
		cleanURL += "&geo=us"
	} else {
		cleanURL += "?geo=us"
	}
	doc, html, err := fetchPage(cleanURL, "xhamster")
	if err != nil {
		return nil, err
	}

	var links []StreamLink
	seen := map[string]bool{}

	// Strategy 1: <link rel=preload> with .m3u8
	doc.Find("link[rel=preload]").Each(func(_ int, el *goquery.Selection) {
		href := el.AttrOr("href", "")
		if strings.Contains(href, ".m3u8") && !seen[href] {
			seen[href] = true
			links = append(links, StreamLink{Src: href, Type: "hls", Label: "HLS Stream"})
		}
	})

	// Strategy 2: window.initials JSON
	initials := extractJsonObject(html, "window.initials")
	if initials != nil {
		if xp, ok := getNestedMap(initials, "xplayerSettings"); ok {
			if hlsURL, ok := getNestedString(xp, "sources", "hls", "h264", "url"); ok && !seen[hlsURL] {
				seen[hlsURL] = true
				links = append(links, StreamLink{Src: hlsURL, Type: "hls", Label: "HLS"})
			}
			if sources, ok := xp["sources"].(map[string]interface{}); ok {
				if standard, ok := sources["standard"].(map[string]interface{}); ok {
					if h264, ok := standard["h264"].([]interface{}); ok {
						for _, q := range h264 {
							if qm, ok := q.(map[string]interface{}); ok {
								if u, ok := qm["url"].(string); ok && u != "" && !seen[u] {
									seen[u] = true
									label, _ := qm["quality"].(string)
									if label == "" {
										label = "MP4"
									}
									links = append(links, StreamLink{Src: u, Type: "mp4", Label: label})
								}
							}
						}
					}
				}
			}
		}
	}

	// Strategy 3: direct m3u8 regex
	if len(links) == 0 {
		m3u8Re := regexp.MustCompile(`https?:[^\s"']+\.m3u8[^\s"']*`)
		matches := dedupe(m3u8Re.FindAllString(html, -1))
		for i, u := range matches {
			links = append(links, StreamLink{Src: u, Type: "hls", Label: fmt.Sprintf("Stream %d", i+1)})
		}
	}

	log.Printf("[xHamster links] found %d links for %s", len(links), pageURL)
	if links == nil {
		links = []StreamLink{}
	}
	return map[string]interface{}{"links": links}, nil
}

// ─── xVideos ──────────────────────────────────────────────────────────────────

func cleanXvHref(href string) string {
	re := regexp.MustCompile(`^(/video\.[^/]+)/[^/]+/[^/]+/(.+)$`)
	if m := re.FindStringSubmatch(href); len(m) == 3 {
		return m[1] + "/" + m[2]
	}
	return href
}

func parseXvCards(doc *goquery.Document, base string) []VideoCard {
	var items []VideoCard
	doc.Find("div.mozaique div.thumb-block").Each(func(_ int, el *goquery.Selection) {
		a := el.Find("p.title a").First()
		title := strings.TrimSpace(a.AttrOr("title", a.Text()))
		rawHref := a.AttrOr("href", "")
		if rawHref == "" || strings.Contains(rawHref, "/channels/") || strings.Contains(rawHref, "/pornstars/") {
			return
		}
		href := absURL(cleanXvHref(rawHref), base)
		img := el.Find("div.thumb a img").First()
		poster := strings.TrimSpace(img.AttrOr("data-src", img.AttrOr("src", "")))
		dur := strings.TrimSpace(el.Find("span.duration").Text())
		if title != "" && href != "" {
			if strings.Contains(poster, "blank") {
				poster = ""
			}
			items = append(items, VideoCard{Title: title, Href: href, Poster: poster, Duration: dur})
		}
	})
	return items
}

func xvHome(page int) map[string]interface{} {
	cfg := sites["xvideos"]
	secs := cfg.Sections
	if len(secs) > 6 {
		secs = secs[:6]
	}
	results := make([]SectionResult, len(secs))
	done := make(chan int, len(secs))
	for i, s := range secs {
		go func(i int, slug, name string) {
			var pageStr string
			if page > 1 {
				pageStr = fmt.Sprintf("/new/%d", page-1)
			}
			var pageURL string
			if slug != "" {
				pageURL = cfg.Base + slug + pageStr
			} else {
				if pageStr == "" {
					pageURL = cfg.Base + "/"
				} else {
					pageURL = cfg.Base + pageStr
				}
			}
			doc, _, err := fetchPage(pageURL, "xvideos")
			if err != nil {
				results[i] = SectionResult{Name: name, Items: []VideoCard{}, Error: err.Error()}
			} else {
				results[i] = SectionResult{Name: name, Items: parseXvCards(doc, cfg.Base)}
			}
			done <- i
		}(i, s.Slug, s.Name)
	}
	for range secs {
		<-done
	}
	return map[string]interface{}{"sections": results}
}

func xvSearch(query string, page int) (map[string]interface{}, error) {
	cfg := sites["xvideos"]
	u := fmt.Sprintf("%s/?k=%s&p=%d", cfg.Base, strings.ReplaceAll(query, " ", "+"), page-1)
	doc, _, err := fetchPage(u, "xvideos")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"items": parseXvCards(doc, cfg.Base)}, nil
}

func xvDetail(pageURL string) (map[string]interface{}, error) {
	doc, html, err := fetchPage(pageURL, "xvideos")
	if err != nil {
		return nil, err
	}

	h2 := doc.Find("h2.page-title").First().Clone()
	h2.Find("span").Remove()
	title := strings.TrimSpace(h2.Text())
	if title == "" {
		title = doc.Find("meta[property='og:title']").AttrOr("content", "Unknown")
	}
	poster := doc.Find("meta[property='og:image']").AttrOr("content", "")
	dur := strings.TrimSpace(doc.Find("h2.page-title span.duration").Text())

	var tags []string
	doc.Find("div.video-tags-list li a.is-keyword, .video-tags a").Each(func(_ int, s *goquery.Selection) {
		if t := strings.TrimSpace(s.Text()); t != "" {
			tags = append(tags, t)
		}
	})

	var recs []VideoCard
	// Find inline script with video_related
	scriptContent := ""
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "video_related") {
			scriptContent = s.Text()
		}
	})
	_ = html // html is available if needed for further extraction

	recRe := regexp.MustCompile(`\{"id":\s*\d+.*?"u"\s*:\s*"(.*?)",\s*"i"\s*:\s*"(.*?)".*?"tf"\s*:\s*"(.*?)"`)
	for _, m := range recRe.FindAllStringSubmatch(scriptContent, -1) {
		rHref := strings.ReplaceAll(m[1], `\/`, "/")
		rImg := strings.ReplaceAll(m[2], `\/`, "/")
		rTitle := unescapeUnicode(m[3])
		if rHref != "" {
			recs = append(recs, VideoCard{Title: rTitle, Href: absURL(rHref, sites["xvideos"].Base), Poster: rImg})
		}
	}

	if tags == nil {
		tags = []string{}
	}
	if recs == nil {
		recs = []VideoCard{}
	}
	return map[string]interface{}{"title": title, "poster": poster, "tags": tags, "duration": dur, "recs": recs, "pageUrl": pageURL}, nil
}

func xvLinks(pageURL string) (map[string]interface{}, error) {
	_, html, err := fetchPage(pageURL, "xvideos")
	if err != nil {
		return nil, err
	}

	var links []StreamLink

	if m := regexp.MustCompile(`html5player\.setVideoHLS\('([^']+)'\)`).FindStringSubmatch(html); len(m) > 1 {
		links = append(links, StreamLink{Src: m[1], Type: "hls", Label: "HLS (best)"})
	}
	hiURL := ""
	if m := regexp.MustCompile(`html5player\.setVideoUrlHigh\('([^']+)'\)`).FindStringSubmatch(html); len(m) > 1 {
		hiURL = m[1]
		links = append(links, StreamLink{Src: m[1], Type: "mp4", Label: "High Quality"})
	}
	if m := regexp.MustCompile(`html5player\.setVideoUrl\('([^']+)'\)`).FindStringSubmatch(html); len(m) > 1 {
		if m[1] != hiURL {
			links = append(links, StreamLink{Src: m[1], Type: "mp4", Label: "Standard"})
		}
	}

	// Fallback CDN pattern
	if len(links) == 0 {
		cdnRe := regexp.MustCompile(`https?://[a-z0-9\-]+\.xvideos-cdn\.com/[^\s"']+\.m3u8`)
		for i, u := range dedupe(cdnRe.FindAllString(html, -1)) {
			links = append(links, StreamLink{Src: u, Type: "hls", Label: fmt.Sprintf("Stream %d", i+1)})
		}
	}

	log.Printf("[xVideos links] found %d links for %s", len(links), pageURL)
	if links == nil {
		links = []StreamLink{}
	}
	return map[string]interface{}{"links": links}, nil
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func unescapeUnicode(s string) string {
	re := regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		var r rune
		fmt.Sscanf(m[2:], "%x", &r)
		if unicode.IsPrint(r) {
			return string(r)
		}
		return m
	})
}

func getNestedMap(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := m[key].(map[string]interface{})
	return v, ok
}

func getNestedString(m map[string]interface{}, keys ...string) (string, bool) {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if s, ok := cur[k].(string); ok {
				return s, true
			}
			return "", false
		}
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			return "", false
		}
		cur = next
	}
	return "", false
}

// ─── Server ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"name": "XWorld API",
			"endpoints": []string{
				"/api/home?site=xvideos", "/api/search?site=xvideos&q=test",
				"/api/detail?site=xvideos&url=VIDEO_URL", "/api/links?site=xvideos&url=VIDEO_URL",
			},
		})
	})

	mux.HandleFunc("/api/home", func(w http.ResponseWriter, r *http.Request) {
		site := strings.ToLower(r.URL.Query().Get("site"))
		if site != "xhamster" && site != "xvideos" {
			writeJSON(w, 400, map[string]string{"error": "site required (xhamster or xvideos)"})
			return
		}
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		if site == "xhamster" {
			writeJSON(w, 200, xhHome(page))
		} else {
			writeJSON(w, 200, xvHome(page))
		}
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		site := strings.ToLower(r.URL.Query().Get("site"))
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if site != "xhamster" && site != "xvideos" {
			writeJSON(w, 400, map[string]string{"error": "site required"})
			return
		}
		if q == "" {
			writeJSON(w, 200, map[string]interface{}{"items": []VideoCard{}})
			return
		}
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		var res map[string]interface{}
		var err error
		if site == "xhamster" {
			res, err = xhSearch(q, page)
		} else {
			res, err = xvSearch(q, page)
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})

	mux.HandleFunc("/api/detail", func(w http.ResponseWriter, r *http.Request) {
		site := strings.ToLower(r.URL.Query().Get("site"))
		u := r.URL.Query().Get("url")
		if u == "" {
			writeJSON(w, 400, map[string]string{"error": "url required"})
			return
		}
		if site != "xhamster" && site != "xvideos" {
			writeJSON(w, 400, map[string]string{"error": "site required"})
			return
		}
		var res map[string]interface{}
		var err error
		if site == "xhamster" {
			res, err = xhDetail(u)
		} else {
			res, err = xvDetail(u)
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})

	mux.HandleFunc("/api/links", func(w http.ResponseWriter, r *http.Request) {
		site := strings.ToLower(r.URL.Query().Get("site"))
		u := r.URL.Query().Get("url")
		if u == "" {
			writeJSON(w, 400, map[string]string{"error": "url required"})
			return
		}
		if site != "xhamster" && site != "xvideos" {
			writeJSON(w, 400, map[string]string{"error": "site required"})
			return
		}
		var res map[string]interface{}
		var err error
		if site == "xhamster" {
			res, err = xhLinks(u)
		} else {
			res, err = xvLinks(u)
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})

	log.Printf("\n🔞  XWorld backend → http://127.0.0.1:%d\n", PORT)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", PORT), mux))
}

package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

const PORT = 1235

// ⚡ FAST HTTP CLIENT
var client = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     30 * time.Second,
	},
}

// ─────────────────────────────────────────
// Categories
var categories = map[string]string{
	"indian":    "https://www.tiava.com/category/indian",
	"hot-mom":   "https://www.tiava.com/category/hot-mom",
	"share-bed": "https://www.tiava.com/category/share-bed",

	"indian-hd":                              "https://www.tiava.com/category/indian-hd",
	"viral":                                  "https://www.tiava.com/category/viral",
	"cute-indian":                            "https://www.tiava.com/category/cute-indian",
	"girlfriend":                             "https://www.tiava.com/category/girlfriend",
	"first-time-indian":                      "https://www.tiava.com/category/first-time-indian",
	"pakistani":                              "https://www.tiava.com/category/pakistani",
	"trans?orientation=straight-and-shemale": "https://www.tiava.com/category/trans?orientation=straight-and-shemale",
	"gay?orientation=straight-and-gay":       "https://www.tiava.com/category/gay?orientation=straight-and-gay",
}


func buildFilter(site string) string {
	return "?filter%5Badvertiser_publish_date%5D=&filter%5Bduration%5D=&filter%5Bquality%5D=&filter%5Bvirtual_reality%5D=&filter%5Badvertiser_site%5D=" + site + "&filter%5Border_by%5D=popular"
}

// ─────────────────────────────────────────
// Fetch

func fetch(u string) (*goquery.Document, string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)

	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	html := string(body)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	return doc, html, err
}

// Decode Tiava

func decodeOutURL(out string) string {
	u, err := url.Parse(out)
	if err != nil {
		return ""
	}

	encoded := u.Query().Get("l")
	if encoded == "" {
		return ""
	}

	decodedURL, _ := url.QueryUnescape(encoded)

	data, err := base64.StdEncoding.DecodeString(decodedURL)
	if err != nil {
		return ""
	}

	str := string(data)

	if strings.Contains(str, "http") {
		return str[strings.Index(str, "http"):]
	}
	return ""
}

// ─────────────────────────────────────────
// Video Struct

type Video struct {
	Title    string `json:"title"`
	Poster   string `json:"poster"`
	Href     string `json:"href"`
	Duration string `json:"duration"`
	Quality  string `json:"quality"`
}

// ─────────────────────────────────────────
// ⚡ PARALLEL SCRAPER

func scrapeTiava(cat, site string) ([]Video, error) {
	base := categories[cat]
	target := base + buildFilter(site)

	doc, _, err := fetch(target)
	if err != nil {
		return nil, err
	}

	var videos []Video
	var wg sync.WaitGroup
	var mu sync.Mutex

	doc.Find(".card").Each(func(i int, s *goquery.Selection) {
		wg.Add(1)

		go func(s *goquery.Selection) {
			defer wg.Done()

			title := strings.TrimSpace(s.Find(".item-title span").Last().Text())
			img, _ := s.Find("img.item-image").Attr("src")
			link, _ := s.Find("a.item-link").Attr("href")

			if title == "" || img == "" || link == "" {
				return
			}

			full := "https://www.tiava.com" + link

			badge := strings.TrimSpace(s.Find(".badge").Text())

			quality := "HD"
			if strings.Contains(badge, "4K") {
				quality = "4K"
			}

			parts := strings.Fields(badge)
			duration := ""
			if len(parts) > 0 {
				duration = parts[len(parts)-1]
			}

			video := Video{
				Title:    title,
				Poster:   img,
				Href:     full,
				Duration: duration,
				Quality:  quality,
			}

			mu.Lock()
			videos = append(videos, video)
			mu.Unlock()
		}(s)
	})

	wg.Wait()
	return videos, nil
}

// ─────────────────────────────────────────
// ⚡ STREAM EXTRACTOR (ONLY WHEN NEEDED)

func extractStream(pageURL string) (string, string) {
	log.Printf("[stream] fetching page: %s", pageURL)

	_, html, err := fetch(pageURL)
	if err != nil {
		log.Printf("[stream] ✗ fetch failed: %v", err)
		return "", ""
	}
	log.Printf("[stream] page fetched (%d bytes)", len(html))

	// HLS via html5player (xhamster standard)
	if m := regexp.MustCompile(`html5player\.setVideoHLS\('([^']+)'\)`).FindStringSubmatch(html); len(m) > 1 {
		log.Printf("[stream] ✓ found HLS (html5player.setVideoHLS): %s", m[1])
		return m[1], "hls"
	}

	// MP4 high via html5player
	if m := regexp.MustCompile(`html5player\.setVideoUrlHigh\('([^']+)'\)`).FindStringSubmatch(html); len(m) > 1 {
		log.Printf("[stream] ✓ found MP4 high (html5player.setVideoUrlHigh): %s", m[1])
		return m[1], "mp4"
	}

	// xhamster JSON sources in window.initials (xhamster45.desi uses this)
	if m := regexp.MustCompile(`"hls":\s*\{[^}]*"url":\s*"([^"]+\.m3u8[^"]*)"`).FindStringSubmatch(html); len(m) > 1 {
		u := strings.ReplaceAll(m[1], `\/`, `/`)
		log.Printf("[stream] ✓ found HLS (window.initials hls.url): %s", u)
		return u, "hls"
	}

	// xplayerSettings sources (used by xhspot/xhamster mirror sites)
	if m := regexp.MustCompile(`xplayerSettings[^{]*\{[\s\S]*?"url"\s*:\s*"([^"]+\.m3u8[^"]*)"`).FindStringSubmatch(html); len(m) > 1 {
		u := strings.ReplaceAll(m[1], `\/`, `/`)
		log.Printf("[stream] ✓ found HLS (xplayerSettings): %s", u)
		return u, "hls"
	}

	// any .m3u8 URL in page
	if m := regexp.MustCompile(`https?://[^\s"'\\]+\.m3u8[^\s"'\\]*`).FindStringSubmatch(html); len(m) > 0 {
		log.Printf("[stream] ✓ found HLS (generic .m3u8 scan): %s", m[0])
		return m[0], "hls"
	}

	// any .mp4 URL in page
	if m := regexp.MustCompile(`https?://[^\s"'\\]+\.mp4[^\s"'\\]*`).FindStringSubmatch(html); len(m) > 0 {
		log.Printf("[stream] ✓ found MP4 (generic .mp4 scan): %s", m[0])
		return m[0], "mp4"
	}

	log.Printf("[stream] ✗ no stream URL found in page. Check if Cloudflare is blocking (page size: %d bytes)", len(html))
	// log first 500 chars to help debug
	preview := html
	if len(preview) > 500 {
		preview = preview[:500]
	}
	log.Printf("[stream] page preview: %s", preview)
	return "", ""
}

// ─────────────────────────────────────────
// API

// ─────────────────────────────────────────
// cors — middleware that adds CORS headers to every response

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		next(w, r)
	}
}

func main() {

	// 🎬 Get Videos (FAST)
	http.HandleFunc("/api/videos", cors(func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("cat")
		site := r.URL.Query().Get("site")

		if cat == "" {
			cat = "indian"
		}
		if site == "" {
			site = "xhamster"
		}

		data, err := scrapeTiava(cat, site)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		json.NewEncoder(w).Encode(data)
	}))

	// 🎯 Direct Play (ONLY when clicked)
	http.HandleFunc("/api/play", cors(func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")

		// smart-decode /out/ URLs
		if strings.Contains(u, "/out/") {
			if real := resolveOutURL(u); real != "" {
				u = real
			}
		}

		// follow redirects for affiliate/partner URLs (e.g. xh.partners)
		u = followRedirect(u)

		stream, typ := extractStream(u)

		json.NewEncoder(w).Encode(map[string]string{
			"url":  stream,
			"type": typ,
		})
	}))

	log.Println("🔥 Ultra Fast Server → http://localhost:1235")
	http.ListenAndServe(":1235", nil)
}

// ─────────────────────────────────────────
// resolveOutURL — extracts real URL from tiava /out/ links using smartDecode

func resolveOutURL(outURL string) string {
	log.Printf("[decode] input /out/ URL: %s", outURL)

	u, err := url.Parse(outURL)
	if err != nil {
		log.Printf("[decode] ✗ failed to parse URL: %v", err)
		return ""
	}
	payload := u.Query().Get("l")
	if payload == "" {
		log.Printf("[decode] ✗ no 'l' param found in URL")
		return ""
	}
	payload, _ = url.QueryUnescape(payload)
	log.Printf("[decode] payload length: %d chars", len(payload))

	results := smartDecode(payload)
	log.Printf("[decode] extracted %d items", len(results))

	for _, item := range results {
		if s, ok := item.(string); ok && strings.HasPrefix(s, "http") {
			log.Printf("[decode] ✓ resolved URL: %s", s)
			return s
		}
	}
	log.Printf("[decode] ✗ no HTTP URL found in decoded results")
	return ""
}

// ─────────────────────────────────────────
// followRedirect — follows 301/302 redirects to get the real final URL
// handles affiliate/partner links like xh.partners that redirect to real pages

var redirectClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
	},
	// do NOT auto-follow — we track manually to log each hop
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func followRedirect(u string) string {
	current := u
	const maxHops = 10

	for i := 0; i < maxHops; i++ {
		parsed, err := url.Parse(current)
		if err != nil {
			log.Printf("[redirect] ✗ bad URL at hop %d: %v", i, err)
			return u // return original on error
		}

		// only follow known redirect domains, not real video hosts
		host := parsed.Hostname()
		if !isRedirectHost(host) {
			if i > 0 {
				log.Printf("[redirect] ✓ final URL after %d hop(s): %s", i, current)
			}
			return current
		}

		log.Printf("[redirect] hop %d → following: %s", i+1, current)

		req, _ := http.NewRequest("GET", current, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Referer", "https://www.tiava.com/")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Sec-CH-UA", `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`)
		req.Header.Set("Sec-CH-UA-Mobile", "?0")
		req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
		req.Header.Set("Upgrade-Insecure-Requests", "1")

		resp, err := redirectClient.Do(req)
		if err != nil {
			log.Printf("[redirect] ✗ request failed at hop %d: %v", i+1, err)
			return current
		}
		resp.Body.Close()

		location := resp.Header.Get("Location")
		if location == "" {
			log.Printf("[redirect] no Location header at hop %d (status %d), stopping", i+1, resp.StatusCode)
			return current
		}

		// resolve relative redirects
		if !strings.HasPrefix(location, "http") {
			base, _ := url.Parse(current)
			rel, _ := url.Parse(location)
			location = base.ResolveReference(rel).String()
		}

		log.Printf("[redirect] hop %d → status %d → redirected to: %s", i+1, resp.StatusCode, location)
		current = location
	}

	log.Printf("[redirect] ✗ max hops (%d) reached, using last URL: %s", maxHops, current)
	return current
}

// isRedirectHost — returns true for known affiliate/partner redirect domains
func isRedirectHost(host string) bool {
	redirectHosts := []string{
		"xh.partners",
		"partners.xhamster.com",
		"go.redirectingat.com",
		"track.adtraction.com",
		"clk.tradedoubler.com",
	}
	for _, h := range redirectHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────
// smartDecode — decodes base64 payload and extracts embedded data

func smartDecode(payload string) []interface{} {
	s := strings.SplitN(payload, "&", 2)[0]
	s = removeWS(s)
	log.Printf("[decode] cleaned payload length: %d (mod4=%d)", len(s), len(s)%4)

	type strategy struct {
		name    string
		urlSafe bool
	}
	for _, st := range []strategy{{"standard", false}, {"url-safe", true}} {
		input := s
		if !st.urlSafe {
			input = strings.NewReplacer("-", "+", "_", "/").Replace(input)
		}
		padded := smartFixPadding(input)

		var decoded []byte
		var err error
		if st.urlSafe {
			decoded, err = base64.URLEncoding.DecodeString(padded)
		} else {
			decoded, err = base64.StdEncoding.DecodeString(padded)
		}
		if err != nil {
			log.Printf("[decode] ✗ %s base64 failed: %v", st.name, err)
			continue
		}
		log.Printf("[decode] ✓ %s base64 succeeded (%d bytes)", st.name, len(decoded))
		return extractEmbedded(decoded)
	}
	log.Printf("[decode] ✗ all base64 strategies failed")
	return nil
}

// extractEmbedded pulls URLs, JSON, and nested base64 from decoded bytes

var (
	reEmbedURL  = regexp.MustCompile(`https?://[^\s\x00-\x1F]+`)
	reEmbedJSON = regexp.MustCompile(`[\{\[][^{}\[\]]*?[\}\]]`)
	reEmbedB64  = regexp.MustCompile(`[A-Za-z0-9+/]{30,}={0,2}`)
	reEmbedCtrl = regexp.MustCompile(`[\x00-\x1F\x7F]`)
)

func extractEmbedded(data []byte) []interface{} {
	var results []interface{}
	seen := map[string]bool{}
	text := strings.ToValidUTF8(string(data), "?")

	// URLs
	for _, u := range reEmbedURL.FindAllString(text, -1) {
		u = reEmbedCtrl.Split(u, 2)[0]
		if !seen[u] {
			seen[u] = true
			results = append(results, u)
		}
	}

	// JSON objects/arrays
	for _, cand := range reEmbedJSON.FindAllString(text, -1) {
		var parsed interface{}
		if err := json.Unmarshal([]byte(cand), &parsed); err == nil && !seen[cand] {
			seen[cand] = true
			results = append(results, parsed)
		}
	}

	// Nested base64
	for _, match := range reEmbedB64.FindAllString(text, -1) {
		inner, err := base64.StdEncoding.DecodeString(match)
		if err != nil {
			continue
		}
		s := string(inner)
		if !smartIsPrintable(s) || seen[s] {
			continue
		}
		seen[s] = true
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			var parsed interface{}
			if err := json.Unmarshal(inner, &parsed); err == nil {
				results = append(results, parsed)
				continue
			}
		}
		results = append(results, s)
	}
	return results
}

func removeWS(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func smartFixPadding(s string) string {
	rem := len(s) % 4
	if rem == 1 {
		s = s[:len(s)-1]
		rem = len(s) % 4
	}
	switch rem {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return s
}

func smartIsPrintable(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

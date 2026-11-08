// Package pornhub finds and resolves videos on pornhub.com.
//
// Only publicly viewable videos are supported. Premium, private, locked,
// geo-blocked and age-verification-gated content is reported as an error,
// never bypassed.
package pornhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// DefaultBase is the clearnet site, reached through a Tor exit.
	DefaultBase = "https://www.pornhub.com"
	// OnionBase is the site's onion service, if it is online.
	OnionBase = "http://pornhubvybmsymdol4iibwgwtkpwmeyd6luq2gxajgjzfjvotyt5zhyd.onion"
)

var (
	ErrNotFound        = errors.New("video not found (deleted, private or needs login)")
	ErrGeoBlocked      = errors.New("video is not available in the Tor exit's country")
	ErrAgeVerification = errors.New("the site demands age verification from this exit; pron-get does not bypass it")
	ErrChallenge       = errors.New("the site served an anti-bot challenge")
	ErrNoFormats       = errors.New("no downloadable formats found")
)

// Client talks to the site. HTTP must already be routed through Tor.
type Client struct {
	HTTP *http.Client
	Base string // scheme://host, DefaultBase if empty
	// NewCircuit, if set, is called before retrying after an anti-bot challenge.
	NewCircuit func()
}

// Video is one resolved video.
type Video struct {
	ID        string
	Title     string
	Uploader  string
	Duration  int // seconds
	Thumbnail string
	PageURL   string
	Formats   []Format // best first
}

// Format is one downloadable rendition.
type Format struct {
	URL    string
	Height int    // 0 when unknown
	Kind   string // "mp4" or "hls"
}

// Result is one search hit.
type Result struct {
	ID       string
	Title    string
	Duration string // as shown on the site, e.g. "12:34"
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return DefaultBase
}

var viewKeyRe = regexp.MustCompile(`^[0-9a-z]{6,32}$`)

// ParseID accepts a bare viewkey or any video URL and returns the viewkey.
func ParseID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if viewKeyRe.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", false
	}
	if k := u.Query().Get("viewkey"); viewKeyRe.MatchString(k) {
		return k, true
	}
	if rest, ok := strings.CutPrefix(u.Path, "/embed/"); ok && viewKeyRe.MatchString(rest) {
		return rest, true
	}
	return "", false
}

// Video fetches the watch page for id and resolves its formats.
func (c *Client) Video(ctx context.Context, id string) (*Video, error) {
	if !viewKeyRe.MatchString(id) {
		return nil, fmt.Errorf("bad viewkey %q", id)
	}
	page := c.base() + "/view_video.php?viewkey=" + id
	body, final, err := c.getPage(ctx, page)
	if err != nil {
		return nil, err
	}
	if final.Query().Get("viewkey") != id {
		return nil, ErrNotFound
	}
	v, defs, err := parseVideoPage(body)
	if err != nil {
		return nil, err
	}
	v.ID = id
	v.PageURL = page

	for _, d := range defs {
		u, err := final.Parse(d.VideoURL)
		if err != nil {
			continue
		}
		if strings.Contains(u.Path, "/video/get_media") {
			var medias []mediaDef
			if err := c.getJSON(ctx, u.String(), &medias); err != nil {
				continue
			}
			for _, m := range medias {
				if mu, err := u.Parse(m.VideoURL); err == nil && m.VideoURL != "" {
					v.Formats = append(v.Formats, makeFormat(mu.String(), m.Format, int(m.Quality)))
				}
			}
			continue
		}
		v.Formats = append(v.Formats, makeFormat(u.String(), d.Format, int(d.Quality)))
	}
	v.Formats = dedupeSort(v.Formats)
	if len(v.Formats) == 0 {
		return nil, ErrNoFormats
	}
	return v, nil
}

// Search returns one page (1-based) of results for query.
func (c *Client) Search(ctx context.Context, query string, page int) ([]Result, error) {
	q := url.Values{"search": {query}}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	body, _, err := c.getPage(ctx, c.base()+"/video/search?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return parseSearch(body), nil
}

// Headers are needed on media requests (the CDN answers 412/403 without them).
func (c *Client) Headers() http.Header {
	h := http.Header{}
	h.Set("Origin", c.base())
	h.Set("Referer", c.base()+"/")
	return h
}

func (c *Client) setCookies() {
	if c.HTTP.Jar == nil {
		return
	}
	u, _ := url.Parse(c.base())
	// The click-through "I am 18 or older" disclaimer, plus desktop layout.
	c.HTTP.Jar.SetCookies(u, []*http.Cookie{
		{Name: "accessAgeDisclaimerPH", Value: "1", Path: "/"},
		{Name: "platform", Value: "pc", Path: "/"},
	})
}

func (c *Client) getPage(ctx context.Context, u string) (string, *url.URL, error) {
	c.setCookies()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 && c.NewCircuit != nil {
			c.NewCircuit()
		}
		body, final, err := c.get(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		if err := pageError(body); err != nil {
			if errors.Is(err, ErrChallenge) && c.NewCircuit != nil {
				lastErr = err
				continue
			}
			return "", nil, err
		}
		return body, final, nil
	}
	return "", nil, lastErr
}

func (c *Client) get(ctx context.Context, u string) (string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		if err := pageError(string(b)); err != nil {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return string(b), resp.Request.URL, nil
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, vs := range c.Headers() {
		req.Header[k] = vs
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get_media: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// ---- parsing (pure functions, unit tested) ----

type mediaDef struct {
	VideoURL string  `json:"videoUrl"`
	Format   string  `json:"format"`
	Quality  flexInt `json:"quality"`
}

type flashvars struct {
	VideoTitle       string     `json:"video_title"`
	ImageURL         string     `json:"image_url"`
	VideoDuration    flexInt    `json:"video_duration"`
	MediaDefinitions []mediaDef `json:"mediaDefinitions"`
}

// flexInt accepts 720, "720", or anything else (as 0).
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		if i, err := strconv.Atoi(strings.SplitN(n.String(), ".", 2)[0]); err == nil {
			*f = flexInt(i)
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		i, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(s), "p"))
		*f = flexInt(i)
	}
	return nil
}

var (
	flashvarsRe  = regexp.MustCompile(`var\s+flashvars_\d+\s*=\s*`)
	twitterTitle = regexp.MustCompile(`<meta[^>]+name=["']twitter:title["'][^>]+content=["']([^"']+)["']`)
	h1Title      = regexp.MustCompile(`(?s)<h1[^>]+class=["']title["'][^>]*>(.+?)</h1>`)
	authorRe     = regexp.MustCompile(`"author"\s*:\s*"([^"]+)"`)
	tagRe        = regexp.MustCompile(`<[^>]+>`)
	spaceRe      = regexp.MustCompile(`\s+`)
	heightInURL  = regexp.MustCompile(`(\d{3,4})[pP]?_\d+[kK]`)

	removedRe   = regexp.MustCompile(`(?s)<div[^>]+class=["'][^"']*\b(?:removed|userMessageSection)\b[^"']*["'][^>]*>(.+?)</div>`)
	noVideoRe   = regexp.MustCompile(`(?s)<section[^>]+class=["']noVideo["'][^>]*>(.+?)</section>`)
	geoRe       = regexp.MustCompile(`class=["']geoBlocked["']|>\s*This content is unavailable in your country`)
	challengeRe = regexp.MustCompile(`<body\b[^>]+\bonload=["']go\(\)|document\.cookie\s*=\s*["']RNKEY=|document\.location\.reload\(true\)`)
	ageVerifyRe = regexp.MustCompile(`(?i)age[- ]verification|verify your age`)
	lockedRe    = regexp.MustCompile(`<[^>]+\bid=["']lockedPlayer`)

	containerRe = regexp.MustCompile(`(?s)<div[^>]+class=["']container.+`)
	searchHitRe = regexp.MustCompile(`href="/?view_video\.php\?[^"]*\bviewkey=([\da-z]+)[^"]*"[^>]*\s+title="([^"]+)"`)
	tileRe      = regexp.MustCompile(`<li[^>]+data-video-vkey="([\da-z]+)"`)
	durationRe  = regexp.MustCompile(`<var[^>]+class="[^"]*\bduration\b[^"]*"[^>]*>\s*([\d:]+)\s*<`)
)

func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(s), " "))
}

// pageError recognises the site's error and interstitial pages.
func pageError(body string) error {
	if challengeRe.MatchString(body) {
		return ErrChallenge
	}
	if geoRe.MatchString(body) {
		return ErrGeoBlocked
	}
	for _, re := range []*regexp.Regexp{removedRe, noVideoRe} {
		if m := re.FindStringSubmatch(body); m != nil {
			if msg := cleanText(m[1]); msg != "" {
				return fmt.Errorf("%w: site says %q", ErrNotFound, msg)
			}
		}
	}
	return nil
}

// extractFlashvars decodes the first `var flashvars_N = {...}` object.
func extractFlashvars(body string) (*flashvars, bool) {
	loc := flashvarsRe.FindStringIndex(body)
	if loc == nil {
		return nil, false
	}
	var fv flashvars
	if err := json.NewDecoder(strings.NewReader(body[loc[1]:])).Decode(&fv); err != nil {
		return nil, false
	}
	return &fv, true
}

func parseVideoPage(body string) (*Video, []mediaDef, error) {
	fv, ok := extractFlashvars(body)
	if !ok {
		if lockedRe.MatchString(body) {
			return nil, nil, fmt.Errorf("%w: video is locked", ErrNotFound)
		}
		if ageVerifyRe.MatchString(body) {
			return nil, nil, ErrAgeVerification
		}
		return nil, nil, errors.New("could not find video data on the page (site layout changed?)")
	}
	v := &Video{
		Thumbnail: fv.ImageURL,
		Duration:  int(fv.VideoDuration),
	}
	if m := twitterTitle.FindStringSubmatch(body); m != nil {
		v.Title = cleanText(m[1])
	} else if m := h1Title.FindStringSubmatch(body); m != nil {
		v.Title = cleanText(m[1])
	} else {
		v.Title = cleanText(fv.VideoTitle)
	}
	if m := authorRe.FindStringSubmatch(body); m != nil {
		v.Uploader = cleanText(m[1])
	}
	return v, fv.MediaDefinitions, nil
}

func makeFormat(u, format string, height int) Format {
	f := Format{URL: u, Height: height, Kind: "mp4"}
	if strings.EqualFold(format, "hls") || strings.Contains(strings.ToLower(u), ".m3u8") {
		f.Kind = "hls"
	}
	if f.Height == 0 {
		if m := heightInURL.FindStringSubmatch(u); m != nil {
			f.Height, _ = strconv.Atoi(m[1])
		}
	}
	return f
}

// dedupeSort removes duplicate URLs and orders best first: higher resolution,
// then mp4 (a single resumable file) before hls.
func dedupeSort(fs []Format) []Format {
	seen := map[string]bool{}
	out := fs[:0]
	for _, f := range fs {
		if !seen[f.URL] {
			seen[f.URL] = true
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Height != out[j].Height {
			return out[i].Height > out[j].Height
		}
		return out[i].Kind == "mp4" && out[j].Kind != "mp4"
	})
	return out
}

// Pick returns the best format no taller than maxHeight (0 = no limit).
func Pick(fs []Format, maxHeight int) (Format, bool) {
	for _, f := range fs {
		if maxHeight == 0 || f.Height <= maxHeight {
			return f, true
		}
	}
	if len(fs) > 0 {
		return fs[len(fs)-1], true
	}
	return Format{}, false
}

func parseSearch(body string) []Result {
	if m := containerRe.FindString(body); m != "" {
		body = m // skip the header drop-down, which links videos too
	}
	var out []Result
	seen := map[string]bool{}
	add := func(r Result) {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}

	// Each hit is an <li data-video-vkey="..."> tile holding the title link
	// and a duration badge.
	tiles := tileRe.FindAllStringSubmatchIndex(body, -1)
	for i, t := range tiles {
		end := len(body)
		if i+1 < len(tiles) {
			end = tiles[i+1][0]
		}
		tile := body[t[0]:end]
		r := Result{ID: body[t[2]:t[3]]}
		if m := searchHitRe.FindStringSubmatch(tile); m != nil {
			r.Title = cleanText(m[2])
		}
		if d := durationRe.FindStringSubmatch(tile); d != nil {
			r.Duration = d[1]
		}
		if r.Title != "" {
			add(r)
		}
	}
	if len(out) > 0 {
		return out
	}

	// Fallback: any view_video link with a title.
	for _, m := range searchHitRe.FindAllStringSubmatch(body, -1) {
		add(Result{ID: m[1], Title: cleanText(m[2])})
	}
	return out
}

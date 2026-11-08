package pornhub

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var siteHostRe = regexp.MustCompile(`^(?:[a-z0-9-]+\.)*pornhub\.(?:com|net|org)$|^pornhubvybmsymdol4iibwgwtkpwmeyd6luq2gxajgjzfjvotyt5zhyd\.onion$`)

// ListingPath turns a URL (or path) of a page that lists videos into a
// site-relative path with query, e.g. "/model/x/videos" or
// "/video/search?search=y". Profile URLs get "/videos" appended. Video URLs
// and other sites are rejected.
func ListingPath(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if u.Host != "" && !siteHostRe.MatchString(strings.ToLower(u.Hostname())) {
		return "", false
	}
	if u.Host == "" && !strings.HasPrefix(u.Path, "/") {
		return "", false
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" || strings.HasPrefix(p, "/view_video.php") || strings.HasPrefix(p, "/embed/") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			return "", false
		}
	}
	switch parts[0] {
	case "model", "pornstar", "channels", "users":
		if len(parts) == 2 {
			p += "/videos"
		}
	case "playlist":
		if len(parts) != 2 {
			return "", false
		}
	}
	q := u.Query()
	q.Del("page")
	if enc := q.Encode(); enc != "" {
		return p + "?" + enc, true
	}
	return p, true
}

// SearchPath is the listing path for a search query.
func SearchPath(query string) string {
	return "/video/search?" + url.Values{"search": {query}}.Encode()
}

var (
	moreRe          = regexp.MustCompile(`<li[^>]+\bclass=["']page_next|<link[^>]+\brel=["']next|<button[^>]+\bid=["']moreDataBtn`)
	playlistIDRe    = regexp.MustCompile(`var\s+playlistId\s*=\s*"(\d+)"`)
	playlistTokenRe = regexp.MustCompile(`var\s+token\s*=\s*"([^"]+)"`)
	playlistCountRe = regexp.MustCompile(`var\s+itemsCount\s*=\s*(\d+)`)
)

// List collects results from a listing path, following pages until limit
// results (0 = everything) or the end. onPage, if set, is called after each
// page with the running total.
func (c *Client) List(ctx context.Context, path string, limit int, onPage func(page, total int)) ([]Result, error) {
	var out []Result
	seen := map[string]bool{}
	add := func(rs []Result) int {
		n := 0
		for _, r := range rs {
			if !seen[r.ID] && (limit == 0 || len(out) < limit) {
				seen[r.ID] = true
				out = append(out, r)
				n++
			}
		}
		return n
	}
	full := func() bool { return limit > 0 && len(out) >= limit }

	first, final, err := c.getPage(ctx, c.base()+path)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("listing %s not found", path)
	}
	if err != nil {
		return nil, err
	}
	// Profiles redirect: a user that is really a model goes to /model/x (the
	// profile page, whose extra tiles are recommendations, not their videos),
	// and a missing profile goes to an index like /pornstars. Follow the first
	// to the same profile's /videos list; report the second as not found.
	reqPath, query, _ := strings.Cut(path, "?")
	if _, isProfile := profileVideos(reqPath); isProfile && final.Path != reqPath {
		target, ok := profileVideos(final.Path)
		if !ok {
			return nil, fmt.Errorf("listing %s not found (the site redirected to %s)", path, final.Path)
		}
		if query != "" {
			target += "?" + query
		}
		if target != path {
			path = target
			if first, _, err = c.getPage(ctx, c.base()+path); err != nil {
				return nil, err
			}
		}
	}
	add(parseSearch(first))
	if onPage != nil {
		onPage(1, len(out))
	}

	if strings.HasPrefix(path, "/playlist/") {
		err := c.playlistRest(ctx, first, add, full, func(page int) {
			if onPage != nil {
				onPage(page, len(out))
			}
		})
		return out, err
	}

	body := first
	for page := 2; !full() && moreRe.MatchString(body); page++ {
		body, _, err = c.getPage(ctx, c.base()+withPage(path, page))
		if errors.Is(err, ErrNotFound) {
			break // ran past the last page
		}
		if err != nil {
			return out, err
		}
		if add(parseSearch(body)) == 0 {
			break
		}
		if onPage != nil {
			onPage(page, len(out))
		}
	}
	return out, nil
}

// playlistRest loads the pages a playlist fetches with JavaScript: the first
// page shows 36 items, then /playlist/viewChunked serves 40 per page.
func (c *Client) playlistRest(ctx context.Context, first string, add func([]Result) int, full func() bool, pageDone func(int)) error {
	id := playlistIDRe.FindStringSubmatch(first)
	token := playlistTokenRe.FindStringSubmatch(first)
	count := playlistCountRe.FindStringSubmatch(first)
	if id == nil || token == nil || count == nil {
		return nil // a short playlist without chunk loading
	}
	n, _ := strconv.Atoi(count[1])
	pages := 1
	if n > 36 {
		pages += (n - 36 + 39) / 40
	}
	for page := 2; page <= pages && !full(); page++ {
		q := url.Values{"id": {id[1]}, "token": {token[1]}, "page": {strconv.Itoa(page)}}
		body, _, err := c.getPage(ctx, c.base()+"/playlist/viewChunked?"+q.Encode())
		if err != nil {
			return err
		}
		if add(parseSearch(body)) == 0 {
			break
		}
		pageDone(page)
	}
	return nil
}

var profileKinds = map[string]bool{"model": true, "pornstar": true, "channels": true, "users": true}

// profileVideos returns the /kind/name/videos listing for a profile path
// (/kind/name or /kind/name/videos).
func profileVideos(p string) (string, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !profileKinds[parts[0]] || parts[1] == "" {
		return "", false
	}
	if len(parts) == 3 && parts[2] != "videos" {
		return "", false
	}
	return "/" + parts[0] + "/" + parts[1] + "/videos", true
}

func withPage(path string, page int) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "page=" + strconv.Itoa(page)
}

package pornhub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestListingPath(t *testing.T) {
	cases := map[string]string{
		"https://www.pornhub.com/model/example-model":               "/model/example-model/videos",
		"https://www.pornhub.com/model/example-model/videos?page=3": "/model/example-model/videos",
		"https://de.pornhub.org/pornstar/x-y/":                      "/pornstar/x-y/videos",
		"https://www.pornhub.com/channels/abc/videos?o=mr":          "/channels/abc/videos?o=mr",
		"/users/someuser": "/users/someuser/videos",
		"https://www.pornhub.com/playlist/123456789":                   "/playlist/123456789",
		"https://www.pornhub.com/video/search?search=two+words&page=2": "/video/search?search=two+words",
		"https://www.pornhub.com/video?c=10":                           "/video?c=10",
		"https://www.pornhub.com/view_video.php?viewkey=abc123":        "",
		"https://www.pornhub.com/embed/abc123":                         "",
		"https://evil.example/model/x":                                 "",
		"https://pornhub.com.evil.example/model/x":                     "",
		"model/x":                  "",
		"https://www.pornhub.com/": "",
		"https://www.pornhub.com/model/../../etc":  "",
		"https://www.pornhub.com/playlist/1/extra": "",
	}
	for in, want := range cases {
		got, ok := ListingPath(in)
		if got != want || ok != (want != "") {
			t.Errorf("ListingPath(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if p := SearchPath("a&b c"); p != "/video/search?search=a%26b+c" {
		t.Errorf("SearchPath = %q", p)
	}
}

func tiles(from, n int) string {
	var b strings.Builder
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, `<li class="pcVideoListItem" data-video-vkey="vid%06d"><a href="/view_video.php?viewkey=vid%06d" title="Video %d"></a><var class="duration">1:00</var></li>`+"\n", i, i, i)
	}
	return b.String()
}

func listServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	// A paged profile: 3 pages of 10, "next" links on the first two.
	mux.HandleFunc("/model/m/videos", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		if page > 3 {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<div class="container">`, tiles((page-1)*10, 10))
		if page < 3 {
			fmt.Fprint(w, `<link rel="next" href="?page=`, page+1, `">`)
		}
	})
	// A playlist: 36 on the page, 40 per chunk, 100 items in total.
	mux.HandleFunc("/playlist/77", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<script>var playlistId = "77"; var itemsCount = 100 || 0; var token = "tok";</script><div class="container">`, tiles(0, 36))
	})
	mux.HandleFunc("/playlist/viewChunked", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("id") != "77" || q.Get("token") != "tok" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		page, _ := strconv.Atoi(q.Get("page"))
		from := 36 + (page-2)*40
		fmt.Fprint(w, tiles(from, min(40, 100-from)))
	})
	// Redirects: a user who is really a model lands on the model's profile
	// page (recommendations, not their videos); a missing pornstar lands on
	// the /pornstars index.
	mux.HandleFunc("/users/m/videos", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/model/m", http.StatusFound)
	})
	mux.HandleFunc("/model/m", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div class="container">`, tiles(900, 5)) // recommendations
	})
	mux.HandleFunc("/pornstar/nobody/videos", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pornstars", http.StatusFound)
	})
	mux.HandleFunc("/pornstars", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div class="container">`, tiles(500, 3))
	})
	return httptest.NewServer(mux)
}

func TestListFollowsProfileRedirects(t *testing.T) {
	srv := listServer(t)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Base: srv.URL}
	ctx := context.Background()

	rs, err := c.List(ctx, "/users/m/videos", 3, nil)
	if err != nil || len(rs) != 3 || rs[0].ID != "vid000000" {
		t.Fatalf("user→model: %+v, %v (want the model's own videos)", rs, err)
	}
	if _, err := c.List(ctx, "/pornstar/nobody/videos", 0, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing pornstar: %v", err)
	}
	if _, ok := profileVideos("/model/m/about"); ok {
		t.Error("/model/m/about is not a video listing")
	}
}

func TestList(t *testing.T) {
	srv := listServer(t)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Base: srv.URL}
	ctx := context.Background()

	rs, err := c.List(ctx, "/model/m/videos", 0, nil)
	if err != nil || len(rs) != 30 || rs[29].ID != "vid000029" {
		t.Fatalf("all pages: %d results, %v", len(rs), err)
	}
	var pages []int
	rs, err = c.List(ctx, "/model/m/videos", 15, func(p, n int) { pages = append(pages, p) })
	if err != nil || len(rs) != 15 || len(pages) != 2 {
		t.Fatalf("limit 15: %d results over pages %v, %v", len(rs), pages, err)
	}
	rs, err = c.List(ctx, "/playlist/77", 0, nil)
	if err != nil || len(rs) != 100 || rs[99].ID != "vid000099" {
		t.Fatalf("playlist: %d results, %v", len(rs), err)
	}
	if _, err := c.List(ctx, "/model/nobody/videos", 0, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing listing: %v", err)
	}
}

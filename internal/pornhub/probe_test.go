package pornhub

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pron-get/pron-get/internal/tor"
)

func probeClient(t *testing.T, ctx context.Context) (*tor.Client, *Client) {
	p, err := tor.Launch(ctx, "", tor.DefaultDataDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	tc := tor.NewClient(p.Addr)
	return tc, &Client{HTTP: tc.Client, NewCircuit: tc.NewCircuit}
}

// PRON_GET_PROBE=viewkey: compare plain vs Range requests on the media CDN.
func TestProbeMedia(t *testing.T) {
	id := os.Getenv("PRON_GET_PROBE")
	if id == "" {
		t.Skip("set PRON_GET_PROBE=<viewkey>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tc, c := probeClient(t, ctx)
	v, err := c.Video(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := Pick(v.Formats, 240)
	t.Logf("url %s", f.URL)
	try := func(label string, mod func(*http.Request)) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
		for k, vs := range c.Headers() {
			req.Header[k] = vs
		}
		mod(req)
		resp, err := tc.Do(req)
		if err != nil {
			t.Logf("%s: %v", label, err)
			return
		}
		resp.Body.Close()
		t.Logf("%s: %s len=%d range=%q", label, resp.Status, resp.ContentLength, resp.Header.Get("Content-Range"))
	}
	try("plain", func(*http.Request) {})
	try("range 1000-1999", func(r *http.Request) { r.Header.Set("Range", "bytes=1000-1999") })
	tc.NewCircuit()
	try("new circuit, plain", func(*http.Request) {})
}

// PRON_GET_PAGES="/path1 /path2" PRON_GET_DUMP=dir: save listing pages and
// report what the parser finds.
func TestProbePages(t *testing.T) {
	paths := strings.Fields(os.Getenv("PRON_GET_PAGES"))
	if len(paths) == 0 {
		t.Skip("set PRON_GET_PAGES")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, c := probeClient(t, ctx)
	dump := os.Getenv("PRON_GET_DUMP")
	more := regexp.MustCompile(`<li[^>]+\bclass=["']page_next|<link[^>]+\brel=["']next|<button[^>]+\bid=["']moreDataBtn`)
	for i, p := range paths {
		body, final, err := c.getPage(ctx, c.base()+p)
		if err != nil {
			t.Logf("%s: %v", p, err)
			continue
		}
		if dump != "" {
			os.WriteFile(filepath.Join(dump, "page"+string(rune('0'+i))+".html"), []byte(body), 0o600)
		}
		rs := parseSearch(body)
		t.Logf("%s -> %s: %d results, more=%v, first=%+v", p, final.Path, len(rs), more.FindString(body), rs[:min(1, len(rs))])
	}
}

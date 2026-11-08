package pornhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pron-get/pron-get/internal/tor"
)

// Run with PRON_GET_LIVE=1 go test ./internal/pornhub -run Live -v
// PRON_GET_DUMP=dir also saves the raw pages for fixture work.
func TestLiveSearchAndResolve(t *testing.T) {
	if os.Getenv("PRON_GET_LIVE") == "" {
		t.Skip("set PRON_GET_LIVE=1 to talk to the real site over Tor")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, err := tor.Launch(ctx, "", tor.DefaultDataDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	tc := tor.NewClient(p.Addr)
	c := &Client{HTTP: tc.Client, NewCircuit: tc.NewCircuit}

	dump := os.Getenv("PRON_GET_DUMP")
	if dump != "" {
		body, _, err := c.getPage(ctx, c.base()+"/video/search?search=massage")
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dump, "search.html"), []byte(body), 0o600)
	}

	res, err := c.Search(ctx, "massage", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d results", len(res))
	if len(res) == 0 {
		t.Fatal("no search results")
	}
	for _, r := range res[:min(3, len(res))] {
		t.Logf("  %s [%s] %q", r.ID, r.Duration, r.Title)
	}

	if dump != "" {
		body, _, err := c.getPage(ctx, c.base()+"/view_video.php?viewkey="+res[0].ID)
		if err == nil {
			os.WriteFile(filepath.Join(dump, "video.html"), []byte(body), 0o600)
		}
	}
	v, err := c.Video(ctx, res[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("video %s %q by %q, %ds", v.ID, v.Title, v.Uploader, v.Duration)
	for _, f := range v.Formats {
		t.Logf("  %s %dp %.80s", f.Kind, f.Height, f.URL)
	}
}

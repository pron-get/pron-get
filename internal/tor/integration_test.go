package tor

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// Run with PRON_GET_LIVE=1 go test ./internal/tor -run Live -v
func TestLiveTor(t *testing.T) {
	if os.Getenv("PRON_GET_LIVE") == "" {
		t.Skip("set PRON_GET_LIVE=1 to talk to the real Tor network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, err := Launch(ctx, "", DefaultDataDir(), os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	c := NewClient(p.Addr)
	r, err := c.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("exit IP %s, IsTor=%v", r.IP, r.IsTor)

	c.NewCircuit()
	r2, err := c.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after NewCircuit: exit IP %s", r2.IP)

	for _, u := range []string{
		"http://pornhubvybmsymdol4iibwgwtkpwmeyd6luq2gxajgjzfjvotyt5zhyd.onion/",
		"https://www.pornhub.com/",
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Logf("%s: %v", u, err)
			continue
		}
		resp.Body.Close()
		t.Logf("%s: %s (final %s)", u, resp.Status, resp.Request.URL)
	}
}

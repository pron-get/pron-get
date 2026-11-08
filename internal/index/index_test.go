package index

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pron-get/pron-get/internal/pornhub"
)

func res(ids ...string) []pornhub.Result {
	var out []pornhub.Result
	for _, id := range ids {
		out = append(out, pornhub.Result{ID: id, Title: "title " + id})
	}
	return out
}

func ids(ix *Index) []string {
	var out []string
	for _, e := range ix.Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestRebuild(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	ix, added := Rebuild(&Index{}, []Fetched{
		{Path: "/a", Results: res("aaaaaa1", "shared1")},
		{Path: "/b", Results: res("shared1", "bbbbbb1")},
	}, t0)
	if added != 3 || len(ix.Entries) != 3 {
		t.Fatalf("first update: added %d, %v", added, ids(ix))
	}
	if e, _ := ix.Get("shared1"); len(e.Sources) != 2 {
		t.Errorf("shared1 sources = %v", e.Sources)
	}

	// Second update: /a has a new video, /b fails (keeps old entries), and
	// a removed source /c is gone.
	ix2, added := Rebuild(ix, []Fetched{
		{Path: "/a", Results: res("aaaaaa2", "aaaaaa1")},
		{Path: "/b", Err: errors.New("timeout")},
	}, t1)
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
	got := ids(ix2)
	want := []string{"aaaaaa2", "aaaaaa1", "shared1", "bbbbbb1"}
	if len(got) != len(want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries %v, want %v", got, want)
		}
	}
	if e, _ := ix2.Get("aaaaaa1"); !e.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen not kept: %v", e.FirstSeen)
	}
	if e, _ := ix2.Get("aaaaaa2"); !e.FirstSeen.Equal(t1) {
		t.Errorf("new entry FirstSeen = %v", e.FirstSeen)
	}

	path := filepath.Join(t.TempDir(), "x", "index.json")
	if err := ix2.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || len(back.Entries) != 4 || !back.Updated.Equal(t1) {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if rs, _ := back.Search("TITLE AAAAAA"); len(rs) != 2 {
		t.Errorf("search = %v", rs)
	}
}

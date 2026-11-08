// Package index is the local package list built by `pron-get update`, the
// equivalent of apt's /var/lib/apt/lists.
package index

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pron-get/pron-get/internal/pornhub"
)

// Entry is one indexed video.
type Entry struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Duration  string    `json:"duration,omitempty"`
	Sources   []string  `json:"sources"` // listing paths it was seen in
	FirstSeen time.Time `json:"first_seen"`
}

// Index is the whole package list, in sources.list order.
type Index struct {
	Updated time.Time `json:"updated"`
	Entries []Entry   `json:"entries"`
}

// DefaultPath is in the user cache dir, next to tor's state.
func DefaultPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "pron-get", "index.json")
}

// Load reads the index. A missing file is an empty index.
func Load(path string) (*Index, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Index{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	return &ix, nil
}

// Save writes the index atomically.
func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ix, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Fetched is what update got for one source. Err means the fetch failed and
// the source's previous entries should be kept.
type Fetched struct {
	Path    string
	Results []pornhub.Result
	Err     error
}

// Rebuild makes a new index from this update's results, in source order.
// Sources that failed keep their entries from old. Entries keep their
// original FirstSeen. It returns the new index and how many IDs are new.
func Rebuild(old *Index, fetched []Fetched, now time.Time) (*Index, int) {
	prev := map[string]Entry{}
	bySource := map[string][]Entry{}
	for _, e := range old.Entries {
		prev[e.ID] = e
		for _, s := range e.Sources {
			bySource[s] = append(bySource[s], e)
		}
	}

	ix := &Index{Updated: now}
	pos := map[string]int{}
	added := 0
	put := func(e Entry, source string) {
		if i, ok := pos[e.ID]; ok {
			ix.Entries[i].Sources = appendUnique(ix.Entries[i].Sources, source)
			return
		}
		pos[e.ID] = len(ix.Entries)
		e.Sources = []string{source}
		ix.Entries = append(ix.Entries, e)
	}
	for _, f := range fetched {
		if f.Err != nil {
			for _, e := range bySource[f.Path] {
				put(e, f.Path)
			}
			continue
		}
		for _, r := range f.Results {
			e := Entry{ID: r.ID, Title: r.Title, Duration: r.Duration, FirstSeen: now}
			if p, ok := prev[r.ID]; ok {
				e.FirstSeen = p.FirstSeen
			} else if _, seen := pos[r.ID]; !seen {
				added++
			}
			put(e, f.Path)
		}
	}
	return ix, added
}

func appendUnique(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

// Get returns the entry for id.
func (ix *Index) Get(id string) (Entry, bool) {
	for _, e := range ix.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Search matches pattern (a case-insensitive regexp, like apt-cache search)
// against IDs and titles.
func (ix *Index) Search(pattern string) ([]Entry, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range ix.Entries {
		if re.MatchString(e.ID) || re.MatchString(e.Title) || re.MatchString(strings.Join(e.Sources, " ")) {
			out = append(out, e)
		}
	}
	return out, nil
}

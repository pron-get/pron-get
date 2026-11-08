// Package sources reads sources.list: the listings `pron-get update` indexes.
//
// One source per line:
//
//	https://www.pornhub.com/model/example-model
//	https://www.pornhub.com/playlist/123456789 limit=100
//	search oil massage limit=20
//
// "#" starts a comment. limit=N caps how many videos are indexed from that
// source (default 40, 0 = everything).
package sources

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pron-get/pron-get/internal/pornhub"
)

// DefaultLimit is how many videos are indexed per source unless limit= says otherwise.
const DefaultLimit = 40

// Source is one parsed line.
type Source struct {
	Line  int
	Raw   string // the line as written, without comments
	Path  string // listing path, e.g. /model/x/videos
	Limit int
}

// DefaultPath is $XDG_CONFIG_HOME/pron-get/sources.list, or ~/.config/....
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "sources.list"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pron-get", "sources.list")
}

// ParseLine parses one line. It returns ok=false for blank and comment lines.
func ParseLine(line string) (Source, bool, error) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Source{}, false, nil
	}
	s := Source{Raw: strings.Join(fields, " "), Limit: DefaultLimit}

	// Trailing key=value options.
	for len(fields) > 1 {
		k, v, ok := strings.Cut(fields[len(fields)-1], "=")
		if !ok {
			break
		}
		switch k {
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return Source{}, false, fmt.Errorf("bad limit %q", v)
			}
			s.Limit = n
		default:
			return Source{}, false, fmt.Errorf("unknown option %q", k)
		}
		fields = fields[:len(fields)-1]
	}

	if fields[0] == "search" {
		if len(fields) < 2 {
			return Source{}, false, errors.New("search needs words")
		}
		s.Path = pornhub.SearchPath(strings.Join(fields[1:], " "))
		return s, true, nil
	}
	if len(fields) != 1 {
		return Source{}, false, fmt.Errorf("expected one URL, got %q", strings.Join(fields, " "))
	}
	p, ok := pornhub.ListingPath(fields[0])
	if !ok {
		return Source{}, false, fmt.Errorf("not a listing URL: %s", fields[0])
	}
	s.Path = p
	return s, true, nil
}

// Parse reads a sources.list. Bad lines are reported together, with line numbers.
func Parse(r io.Reader) ([]Source, error) {
	var out []Source
	var errs []error
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		s, ok, err := ParseLine(sc.Text())
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", n, err))
			continue
		}
		if ok {
			s.Line = n
			out = append(out, s)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, errors.Join(errs...)
}

// Load parses the file at path. A missing file is created from Template.
func Load(path string) ([]Source, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(path, []byte(Template), 0o600)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ss, err := Parse(f)
	if err != nil {
		return ss, fmt.Errorf("%s: %w", path, err)
	}
	return ss, nil
}

// Add validates line and appends it to the file at path.
func Add(path, line string) (Source, error) {
	s, ok, err := ParseLine(line)
	if err != nil {
		return Source{}, err
	}
	if !ok {
		return Source{}, errors.New("empty source")
	}
	existing, err := Load(path)
	if err != nil {
		return Source{}, err
	}
	for _, e := range existing {
		if e.Path == s.Path {
			return e, fmt.Errorf("%s is already in %s (line %d)", s.Path, path, e.Line)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Source{}, err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, s.Raw)
	return s, err
}

// Template is written when no sources.list exists yet.
const Template = `#
# pron-get sources.list
#
# One source per line: a listing URL (model, pornstar, channel, user,
# playlist, category) or "search <words>". Add limit=N to change how many
# videos are indexed from it (default 40, 0 = all).
#
# Then: pron-get update && pron-get upgrade
#
#https://www.pornhub.com/model/example-model
#https://www.pornhub.com/playlist/123456789 limit=100
#search oil massage limit=20
`

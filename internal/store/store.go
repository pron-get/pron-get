// Package store keeps track of installed "packages": one directory per video
// under the root, holding the media file and a pkg.json manifest.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Package is the manifest of one installed video.
type Package struct {
	Name        string    `json:"name"` // the viewkey
	Title       string    `json:"title"`
	Uploader    string    `json:"uploader,omitempty"`
	Duration    int       `json:"duration,omitempty"`
	Source      string    `json:"source"` // page URL
	Height      int       `json:"height,omitempty"`
	File        string    `json:"file"` // relative to the package dir
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// Store is a directory of packages.
type Store struct{ Root string }

var nameRe = regexp.MustCompile(`^[0-9a-z]{6,32}$`)

// ErrBadName guards against path traversal: porn-get 0.5.1 ran
// `rm -rf ${dir}$2` on whatever you typed.
var ErrBadName = errors.New("invalid package name")

// DefaultRoot is ~/pr0n, as in porn-getrc.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "pr0n"
	}
	return filepath.Join(home, "pr0n")
}

// Dir returns the directory for package name, creating nothing.
func (s Store) Dir(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("%w %q", ErrBadName, name)
	}
	return filepath.Join(s.Root, name), nil
}

// Prepare creates the package directory (0700) and returns it.
func (s Store) Prepare(name string) (string, error) {
	dir, err := s.Dir(name)
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// Save writes the manifest.
func (s Store) Save(p Package) error {
	dir, err := s.Dir(p.Name)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".pkg.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "pkg.json"))
}

// Get loads one installed package.
func (s Store) Get(name string) (*Package, error) {
	dir, err := s.Dir(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "pkg.json"))
	if err != nil {
		return nil, err
	}
	var p Package
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &p, nil
}

// Installed reports whether name has a manifest.
func (s Store) Installed(name string) bool {
	_, err := s.Get(name)
	return err == nil
}

// List returns all installed packages, newest first. Directories without a
// manifest (interrupted installs) are skipped.
func (s Store) List() ([]Package, error) {
	ents, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Package
	for _, e := range ents {
		if !e.IsDir() || !nameRe.MatchString(e.Name()) {
			continue
		}
		if p, err := s.Get(e.Name()); err == nil {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstalledAt.After(out[j].InstalledAt) })
	return out, nil
}

// Remove deletes a package directory. It only ever removes a validated
// directory directly under Root.
func (s Store) Remove(name string) error {
	dir, err := s.Dir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("package %s is not installed", name)
	}
	return os.RemoveAll(dir)
}

// removedFile lists packages the user removed, so `upgrade` doesn't bring
// them back (dpkg's "deinstall" state).
const removedFile = ".removed"

// Removed returns the set of packages marked as removed.
func (s Store) Removed() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(s.Root, removedFile))
	if err != nil {
		return out
	}
	for _, l := range strings.Fields(string(b)) {
		if nameRe.MatchString(l) {
			out[l] = true
		}
	}
	return out
}

// SetRemoved marks (or unmarks) packages as removed.
func (s Store) SetRemoved(removed bool, names ...string) error {
	set := s.Removed()
	for _, n := range names {
		if !nameRe.MatchString(n) {
			return fmt.Errorf("%w %q", ErrBadName, n)
		}
		if removed {
			set[n] = true
		} else {
			delete(set, n)
		}
	}
	list := make([]string, 0, len(set))
	for n := range set {
		list = append(list, n)
	}
	sort.Strings(list)
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Root, removedFile), []byte(strings.Join(list, "\n")+"\n"), 0o600)
}

// DirSize sums file sizes under the package directory.
func (s Store) DirSize(name string) int64 {
	dir, err := s.Dir(name)
	if err != nil {
		return 0
	}
	var n int64
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Slug makes a short, filesystem-safe file name from a title.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "video"
	}
	return s
}

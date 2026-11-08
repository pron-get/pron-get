package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLifecycle(t *testing.T) {
	root := t.TempDir()
	s := Store{Root: root}
	dir, err := s.Prepare("abc123def")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "v.mp4"), []byte("1234"), 0o600)
	if err := s.Save(Package{Name: "abc123def", Title: "T", File: "v.mp4", InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "half00install"), 0o700) // no manifest: skipped

	ps, err := s.List()
	if err != nil || len(ps) != 1 || ps[0].Title != "T" {
		t.Fatalf("List = %+v, %v", ps, err)
	}
	if n := s.DirSize("abc123def"); n < 4 {
		t.Errorf("DirSize = %d", n)
	}
	if err := s.Remove("abc123def"); err != nil {
		t.Fatal(err)
	}
	if s.Installed("abc123def") {
		t.Error("still installed")
	}
}

func TestRemoveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(filepath.Dir(root), "victim-"+filepath.Base(root))
	os.MkdirAll(victim, 0o700)
	defer os.RemoveAll(victim)
	s := Store{Root: root}
	for _, name := range []string{"../" + filepath.Base(victim), "", ".", "..", "/etc", "a/b/c/d/e"} {
		if err := s.Remove(name); !errors.Is(err, ErrBadName) {
			t.Errorf("Remove(%q) = %v", name, err)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("victim directory was removed")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Hello, World!":          "hello-world",
		"  --émoji 👀 test-- ":    "moji-test",
		"":                       "video",
		"A\"quoted\" / path\\..": "a-quoted-path",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemovedMarks(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.SetRemoved(true, "abc123def", "zzz999zzz"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRemoved(false, "zzz999zzz"); err != nil {
		t.Fatal(err)
	}
	r := s.Removed()
	if !r["abc123def"] || r["zzz999zzz"] || len(r) != 1 {
		t.Errorf("Removed = %v", r)
	}
	if err := s.SetRemoved(true, "../x"); !errors.Is(err, ErrBadName) {
		t.Errorf("bad name: %v", err)
	}
}

package tor

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDirLocked(t *testing.T) {
	dir := t.TempDir()
	if dirLocked(dir) {
		t.Fatal("no lock file: should be free")
	}
	f, err := os.Create(filepath.Join(dir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if dirLocked(dir) {
		t.Fatal("unlocked file reported locked")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if !dirLocked(dir) {
		t.Fatal("held lock not detected")
	}
}

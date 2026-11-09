package tor

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Process is a private tor instance started by pron-get.
type Process struct {
	Addr    string
	cmd     *exec.Cmd
	tempDir string // removed on Close, when the shared dataDir was busy
}

// Launch starts the tor binary on a free local port and waits until it has
// bootstrapped. Guard and consensus state is kept in dataDir so later runs
// start quickly and keep the same entry guards (which is what Tor wants).
// The process exits when pron-get does.
func Launch(ctx context.Context, torBin, dataDir string, log io.Writer) (*Process, error) {
	if torBin == "" {
		torBin = "tor"
	}
	path, err := exec.LookPath(torBin)
	if err != nil {
		return nil, fmt.Errorf("tor binary not found (install with `brew install tor` or pass --tor-bin): %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	// Another pron-get's tor holds dataDir: use a throwaway one instead of
	// failing (it bootstraps more slowly, but works).
	var tempDir string
	if dirLocked(dataDir) {
		var err error
		if tempDir, err = os.MkdirTemp(filepath.Dir(dataDir), "tor-"); err != nil {
			return nil, err
		}
		dataDir = tempDir
	}
	cleanup := func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)

	cmd := exec.CommandContext(ctx, path,
		// ignore any torrc on the system
		"-f", filepath.Join(dataDir, "torrc-unused"), "--ignore-missing-torrc",
		"--SocksPort", addr+" ExtendedErrors IsolateSOCKSAuth",
		"--SafeSocks", "1", // refuse requests carrying raw IPs: DNS must go through tor
		"--DataDirectory", dataDir,
		"--ClientOnly", "1",
		"--AvoidDiskWrites", "1",
		"--__OwningControllerProcess", strconv.Itoa(os.Getpid()),
		"--Log", "notice stdout",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}

	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		var last string
		for sc.Scan() {
			line := sc.Text()
			if i := strings.Index(line, "Bootstrapped "); i >= 0 {
				msg := line[i:]
				if msg != last && log != nil {
					fmt.Fprintf(log, "tor: %s\n", msg)
				}
				last = msg
				if strings.HasPrefix(msg, "Bootstrapped 100%") {
					ready <- nil
					io.Copy(io.Discard, stdout) // keep draining
					return
				}
			} else if strings.Contains(line, "[err]") {
				last = line
			}
		}
		ready <- fmt.Errorf("tor exited before bootstrapping: %s", last)
	}()

	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	select {
	case err := <-ready:
		if err != nil {
			cmd.Wait()
			cleanup()
			return nil, err
		}
	case <-timeout.C:
		cmd.Process.Kill()
		cmd.Wait()
		cleanup()
		return nil, errors.New("tor did not bootstrap within 3 minutes")
	case <-ctx.Done():
		cmd.Wait()
		cleanup()
		return nil, ctx.Err()
	}
	return &Process{Addr: addr, cmd: cmd, tempDir: tempDir}, nil
}

// Close stops the tor process.
func (p *Process) Close() error {
	if p == nil || p.cmd.Process == nil {
		return nil
	}
	p.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		p.cmd.Process.Kill()
		<-done
	}
	if p.tempDir != "" {
		os.RemoveAll(p.tempDir)
	}
	return nil
}

// dirLocked reports whether a running tor holds dataDir's lock file.
func dirLocked(dataDir string) bool {
	f, err := os.OpenFile(filepath.Join(dataDir, "lock"), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// DefaultDataDir is where the private tor instance keeps its state.
func DefaultDataDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "pron-get", "tor")
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomToken() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

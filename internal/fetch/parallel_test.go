package fetch

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rangeServer serves data with Range support. URLs containing "bad" get the
// CDN's wrong-IP answer, like a signed URL used from another exit.
func rangeServer(t *testing.T, data []byte, ranges *[]string) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(StatusWrongIP)
			return
		}
		if ranges != nil && r.Header.Get("Range") != "bytes=0-0" {
			mu.Lock()
			*ranges = append(*ranges, r.Header.Get("Range"))
			mu.Unlock()
		}
		http.ServeContent(w, r, "v.mp4", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testData(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(b)
	return b
}

func TestParallel(t *testing.T) {
	data := testData(2*ChunkSize + 12345)
	srv := rangeServer(t, data, nil)
	var resolves atomic.Int32
	resolve := func(context.Context) (Source, error) {
		i := resolves.Add(1)
		return Source{srv.Client(), fmt.Sprintf("%s/good%d.mp4", srv.URL, i)}, nil
	}
	dst := filepath.Join(t.TempDir(), "v.mp4")
	var last int64
	err := Parallel(context.Background(), Source{srv.Client(), srv.URL + "/bad.mp4"}, resolve, 4, dst, nil,
		func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: %d bytes", len(got))
	}
	// 1 to replace the bad first source, then 2 more workers (3 pieces).
	if n := resolves.Load(); n != 3 {
		t.Errorf("resolved %d times, want 3", n)
	}
	if last <= 0 || last > int64(len(data)) {
		t.Errorf("progress ended at %d", last)
	}
	for _, p := range []string{dst + ".part", dst + ".part.map"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestParallelResume(t *testing.T) {
	data := testData(3*ChunkSize + 7)
	var ranges []string
	srv := rangeServer(t, data, &ranges)
	src := Source{srv.Client(), srv.URL + "/v.mp4"}
	resolve := func(context.Context) (Source, error) { return src, nil }

	// A .part from File: the first piece and a bit is already there.
	dir := t.TempDir()
	dst := filepath.Join(dir, "v.mp4")
	os.WriteFile(dst+".part", data[:ChunkSize+100], 0o600)
	if err := Parallel(context.Background(), src, resolve, 1, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		fmt.Sprintf("bytes=%d-%d", ChunkSize, 2*ChunkSize-1),
		fmt.Sprintf("bytes=%d-%d", 2*ChunkSize, 3*ChunkSize-1),
		fmt.Sprintf("bytes=%d-%d", 3*ChunkSize, 3*ChunkSize+6),
	}
	if fmt.Sprint(ranges) != fmt.Sprint(want) {
		t.Errorf("ranges = %v, want %v", ranges, want)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}

	// A map from an earlier Parallel run: pieces 0 and 2 are done.
	ranges = nil
	dst = filepath.Join(dir, "w.mp4")
	part := make([]byte, len(data))
	copy(part[:ChunkSize], data)
	copy(part[2*ChunkSize:3*ChunkSize], data[2*ChunkSize:])
	os.WriteFile(dst+".part", part, 0o600)
	os.WriteFile(dst+".part.map", fmt.Appendf(nil, "size %d\n0\n2\n", len(data)), 0o600)
	if err := Parallel(context.Background(), src, resolve, 2, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	slices.Sort(ranges) // two workers: either may ask first
	if len(ranges) != 2 || ranges[0] != want[2] || ranges[1] != want[0] {
		t.Errorf("ranges = %v", ranges)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestParallelNoRanges(t *testing.T) {
	data := testData(1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data) // ignores Range
	}))
	defer srv.Close()
	src := Source{srv.Client(), srv.URL}
	dst := filepath.Join(t.TempDir(), "v.mp4")
	resolve := func(context.Context) (Source, error) { t.Error("resolve called"); return src, nil }
	if err := Parallel(context.Background(), src, resolve, 4, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestParallelGivesUp(t *testing.T) {
	srv := rangeServer(t, testData(100), nil)
	bad := Source{srv.Client(), srv.URL + "/bad.mp4"}
	resolve := func(context.Context) (Source, error) { return bad, nil }
	err := Parallel(context.Background(), bad, resolve, 2, filepath.Join(t.TempDir(), "v.mp4"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "different exit IP") {
		t.Fatalf("err = %v", err)
	}
}

// stallingServer serves data with Range support, except that the first
// request for want sends a little and then goes silent without closing,
// like a Tor circuit that stops delivering.
func stallingServer(t *testing.T, data []byte, want string) *httptest.Server {
	var stalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == want && stalled.CompareAndSwap(false, true) {
			http.ServeContent(&stallWriter{ResponseWriter: w, left: 1000}, r, "v.mp4", time.Time{}, bytes.NewReader(data))
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "v.mp4", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stallWriter passes on the first left bytes of the body, then blocks
// until the client goes away.
type stallWriter struct {
	http.ResponseWriter
	left int
}

func (s *stallWriter) Write(p []byte) (int, error) {
	if s.left <= 0 {
		return 0, http.ErrHandlerTimeout // stop ServeContent; the handler then waits
	}
	n := min(len(p), s.left)
	s.left -= n
	n, err := s.ResponseWriter.Write(p[:n])
	s.ResponseWriter.(http.Flusher).Flush()
	return n, err
}

func shortStall(t *testing.T) {
	old := StallTimeout
	StallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { StallTimeout = old })
}

func TestParallelStall(t *testing.T) {
	shortStall(t)
	data := testData(4*ChunkSize + 99)
	// A full piece near the end, so the other workers have run out of work
	// by the time it stalls and must wait for it.
	srv := stallingServer(t, data, fmt.Sprintf("bytes=%d-%d", 3*ChunkSize, 4*ChunkSize-1))
	resolve := func(context.Context) (Source, error) {
		return Source{srv.Client(), srv.URL + "/v.mp4"}, nil
	}
	dst := filepath.Join(t.TempDir(), "v.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Parallel(ctx, Source{srv.Client(), srv.URL + "/v.mp4"}, resolve, 4, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: %d bytes", len(got))
	}
}

func TestFileStall(t *testing.T) {
	shortStall(t)
	data := testData(50000)
	srv := stallingServer(t, data, "")
	dst := filepath.Join(t.TempDir(), "v.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := File(ctx, srv.Client(), srv.URL+"/v.mp4", dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: %d bytes", len(got))
	}
}

func TestChunkQueueWaitsForInflight(t *testing.T) {
	q := newChunkQueue()
	q.put(0)
	c, _ := q.take(context.Background())
	got := make(chan int)
	go func() {
		c, ok := q.take(context.Background())
		if !ok {
			c = -1
		}
		got <- c
	}()
	select {
	case c := <-got:
		t.Fatalf("take returned %d while a piece was in flight", c)
	case <-time.After(50 * time.Millisecond):
	}
	q.retry(c)
	if c := <-got; c != 0 {
		t.Fatalf("take got %d, want the retried piece 0", c)
	}
	q.done()
	if _, ok := q.take(context.Background()); ok {
		t.Fatal("take succeeded on an empty, idle queue")
	}
}

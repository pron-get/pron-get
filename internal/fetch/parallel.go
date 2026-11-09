package fetch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ChunkSize is how much one connection fetches before taking the next piece.
const ChunkSize = 8 << 20

// Source is a media URL and the client allowed to fetch it. Signed CDN URLs
// only work from the Tor exit they were issued to, so the two go together.
type Source struct {
	Client Getter
	URL    string
}

// Resolver returns a new Source on a circuit no other Source uses.
type Resolver func(ctx context.Context) (Source, error)

// Parallel downloads url to dst over n connections, each on its own Tor
// circuit. Tor limits throughput per circuit, not per exit, so this adds up.
// The file is fetched in ChunkSize pieces from a shared queue, so fast
// circuits take more of them. Finished pieces are listed in dst+".part.map",
// and an interrupted download resumes from there (or from a .part left by
// File). If the server doesn't do Range requests, it falls back to File.
func Parallel(ctx context.Context, first Source, resolve Resolver, n int, dst string, h http.Header, progress Progress) error {
	src := first
	var total int64
	for tries := 0; ; tries++ {
		var err error
		total, err = probeSize(ctx, src, h)
		if err == nil {
			break
		}
		if errors.Is(err, errNoRanges) {
			// File would take a preallocated .part for a finished one.
			if _, err := os.Stat(dst + ".part.map"); err == nil {
				os.Remove(dst + ".part")
				os.Remove(dst + ".part.map")
			}
			return File(ctx, src.Client, src.URL, dst, h, progress)
		}
		if !errors.Is(err, ErrWrongIP) || tries == 4 {
			return err
		}
		if src, err = resolve(ctx); err != nil {
			return err
		}
	}

	part, mapPath := dst+".part", dst+".part.map"
	done, err := loadMap(mapPath, part, total)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(total); err != nil {
		return err
	}
	m, err := os.OpenFile(mapPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer m.Close()
	if fi, _ := m.Stat(); fi.Size() == 0 {
		fmt.Fprintf(m, "size %d\n", total)
		for c := range done {
			fmt.Fprintln(m, c)
		}
	}

	nChunks := int((total + ChunkSize - 1) / ChunkSize)
	q := &chunkQueue{}
	var have int64
	for c := 0; c < nChunks; c++ {
		if done[c] {
			have += chunkLen(c, total)
		} else {
			q.put(c)
		}
	}
	pr := &sharedProgress{fn: progress, done: have, total: total}
	pr.add(0)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		lastErr error
	)
	workers := min(n, q.len())
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := pullChunks(ctx, f, q, w, src, resolve, total, h, pr, func(c int) error {
				mu.Lock()
				defer mu.Unlock()
				_, err := fmt.Fprintln(m, c)
				return err
			})
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if left := q.len(); left > 0 {
		return fmt.Errorf("%d of %d pieces not downloaded: %w", left, nChunks, lastErr)
	}
	pr.flush()
	if err := f.Close(); err != nil {
		return err
	}
	m.Close()
	os.Remove(mapPath)
	return os.Rename(part, dst)
}

// pullChunks is one connection's loop: take a piece, fetch it, record it.
// Worker 0 starts on first; the others resolve their own Source. A refused
// URL (wrong exit IP, expired) or repeated errors move it to a new circuit.
func pullChunks(ctx context.Context, f *os.File, q *chunkQueue, w int, first Source, resolve Resolver, total int64, h http.Header, pr *sharedProgress, record func(int) error) error {
	src := first
	if w > 0 {
		var err error
		if src, err = resolve(ctx); err != nil {
			return err
		}
	}
	failures, circuits := 0, 1
	for {
		c, ok := q.take()
		if !ok {
			return nil
		}
		err := fetchChunk(ctx, src, f, c, total, h, pr)
		if err == nil {
			failures = 0
			if err := record(c); err != nil {
				q.put(c)
				return err
			}
			continue
		}
		q.put(c)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures++
		var perm permanentError
		if errors.As(err, &perm) || failures >= 3 {
			if circuits == 5 {
				return err
			}
			circuits++
			failures = 0
			if src, err = resolve(ctx); err != nil {
				return err
			}
			continue
		}
		if err := sleep(ctx, time.Duration(failures)*2*time.Second); err != nil {
			return err
		}
	}
}

func fetchChunk(ctx context.Context, src Source, f *os.File, c int, total int64, h http.Header, pr *sharedProgress) error {
	start := int64(c) * ChunkSize
	n := chunkLen(c, total)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return permanentError{err}
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+n-1))
	resp, err := src.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return statusError(resp)
	}
	w := &offsetWriter{f: f, off: start, pr: pr}
	got, err := io.Copy(w, io.LimitReader(resp.Body, n))
	if err == nil && got != n {
		err = fmt.Errorf("short piece: %d of %d bytes", got, n)
	}
	if err != nil {
		pr.add(-got) // the piece will be fetched again
	}
	return err
}

var errNoRanges = errors.New("server does not support Range requests")

// probeSize asks for the first byte to learn the file size.
func probeSize(ctx context.Context, src Source, h http.Header) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return 0, err
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := src.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if t := rangeTotal(resp.Header.Get("Content-Range"), -1); t > 0 {
			return t, nil
		}
		return 0, errNoRanges
	case http.StatusOK:
		return 0, errNoRanges
	}
	return 0, statusError(resp)
}

// statusError classifies an unexpected response like fileOnce does.
func statusError(resp *http.Response) error {
	err := fmt.Errorf("%s", resp.Status)
	switch resp.StatusCode {
	case StatusWrongIP:
		return permanentError{ErrWrongIP}
	case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return permanentError{fmt.Errorf("media URL refused (%w); it may have expired", err)}
	}
	return err
}

// loadMap returns the pieces already downloaded. Without a map, a .part from
// File holds a contiguous prefix, and every whole piece in it counts.
func loadMap(mapPath, part string, total int64) (map[int]bool, error) {
	done := map[int]bool{}
	mf, err := os.Open(mapPath)
	if errors.Is(err, os.ErrNotExist) {
		if fi, err := os.Stat(part); err == nil && fi.Size() <= total {
			for c := 0; int64(c+1)*ChunkSize <= fi.Size(); c++ {
				done[c] = true
			}
			if fi.Size() == total && total%ChunkSize != 0 {
				done[int(total/ChunkSize)] = true
			}
		}
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	defer mf.Close()
	sc := bufio.NewScanner(mf)
	if !sc.Scan() || sc.Text() != fmt.Sprintf("size %d", total) {
		// A different file (or a damaged map): start over.
		mf.Close()
		os.Remove(mapPath)
		os.Remove(part)
		return done, nil
	}
	for sc.Scan() {
		if c, err := strconv.Atoi(strings.TrimSpace(sc.Text())); err == nil {
			done[c] = true
		}
	}
	return done, sc.Err()
}

func chunkLen(c int, total int64) int64 {
	return min(ChunkSize, total-int64(c)*ChunkSize)
}

type chunkQueue struct {
	mu sync.Mutex
	cs []int
}

func (q *chunkQueue) put(c int) {
	q.mu.Lock()
	q.cs = append(q.cs, c)
	q.mu.Unlock()
}

// take returns the lowest-numbered piece left, so the file fills from the
// front and a partial download is mostly playable.
func (q *chunkQueue) take() (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.cs) == 0 {
		return 0, false
	}
	low := 0
	for i, c := range q.cs {
		if c < q.cs[low] {
			low = i
		}
	}
	c := q.cs[low]
	q.cs = append(q.cs[:low], q.cs[low+1:]...)
	return c, true
}

func (q *chunkQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.cs)
}

// sharedProgress sums bytes from all connections into one Progress callback.
type sharedProgress struct {
	mu          sync.Mutex
	fn          Progress
	done, total int64
	last        time.Time
}

func (p *sharedProgress) add(n int64) {
	if p.fn == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	if time.Since(p.last) > 200*time.Millisecond {
		p.last = time.Now()
		p.fn(p.done, p.total)
	}
}

// flush reports the current count even if one was just reported.
func (p *sharedProgress) flush() {
	p.last = time.Time{}
	p.add(0)
}

type offsetWriter struct {
	f   *os.File
	off int64
	pr  *sharedProgress
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	w.pr.add(int64(n))
	return n, err
}

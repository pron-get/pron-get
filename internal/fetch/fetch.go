// Package fetch downloads media files: plain HTTP files with resume, and HLS
// playlists segment by segment.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Progress is called as bytes arrive. total is -1 when unknown.
type Progress func(done, total int64)

// Getter is the slice of *http.Client that fetch needs.
type Getter interface {
	Do(*http.Request) (*http.Response, error)
}

// File downloads url to dst. Partial data is kept in dst+".part" and resumed
// with a Range request on the next call.
func File(ctx context.Context, c Getter, url, dst string, h http.Header, progress Progress) error {
	part := dst + ".part"
	var offset int64
	if fi, err := os.Stat(part); err == nil {
		offset = fi.Size()
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			if fi, err := os.Stat(part); err == nil {
				offset = fi.Size()
			}
			if err := sleep(ctx, time.Duration(attempt)*2*time.Second); err != nil {
				return err
			}
		}
		done, err := fileOnce(ctx, c, url, part, offset, h, progress)
		if err == nil && done {
			return os.Rename(part, dst)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var perm permanentError
		if errors.As(err, &perm) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("download failed after retries: %w", lastErr)
}

type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

// StatusWrongIP is what the phncdn CDN answers when a signed media URL is
// fetched from a different IP than the one it was issued to.
const StatusWrongIP = 474

// ErrWrongIP means the media URL is bound to another IP. Over Tor this happens
// when an exit uses several outbound addresses; resolve again on a new circuit.
var ErrWrongIP = errors.New("media URL is bound to a different exit IP")

func fileOnce(ctx context.Context, c Getter, url, part string, offset int64, h http.Header, progress Progress) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, permanentError{err}
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	total := int64(-1)
	switch resp.StatusCode {
	case http.StatusOK:
		flags |= os.O_TRUNC // server ignored Range: start over
		offset = 0
		total = resp.ContentLength
	case http.StatusPartialContent:
		flags |= os.O_APPEND
		total = rangeTotal(resp.Header.Get("Content-Range"), offset+resp.ContentLength)
	case http.StatusRequestedRangeNotSatisfiable:
		// Usually means the .part is already complete.
		if t := rangeTotal(resp.Header.Get("Content-Range"), -1); t == offset {
			return true, nil
		}
		os.Remove(part)
		return false, errors.New("resume rejected, restarting")
	default:
		err := fmt.Errorf("%s", resp.Status)
		if resp.StatusCode == StatusWrongIP {
			return false, permanentError{ErrWrongIP}
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			// Signed CDN links expire; the caller must re-resolve the video.
			return false, permanentError{fmt.Errorf("media URL refused (%w); it may have expired", err)}
		}
		return false, err
	}

	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return false, permanentError{err}
	}
	defer f.Close()
	w := &countingWriter{w: f, n: offset, total: total, progress: progress}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return false, err
	}
	if total >= 0 && w.n != total {
		return false, fmt.Errorf("short download: %d of %d bytes", w.n, total)
	}
	return true, nil
}

// rangeTotal reads the total from "bytes a-b/total".
func rangeTotal(cr string, fallback int64) int64 {
	if i := strings.LastIndexByte(cr, '/'); i >= 0 {
		if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

type countingWriter struct {
	w        io.Writer
	n, total int64
	progress Progress
	last     time.Time
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.progress != nil && time.Since(c.last) > 200*time.Millisecond {
		c.last = time.Now()
		c.progress(c.n, c.total)
	}
	return n, err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

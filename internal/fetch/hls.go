package fetch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Variant is one entry of an HLS master playlist.
type Variant struct {
	URL       string
	Bandwidth int
	Height    int
}

// Segment is one media segment, with its decryption key if any.
type Segment struct {
	URL string
	Key *Key
	Seq uint64
}

// Key describes #EXT-X-KEY METHOD=AES-128.
type Key struct {
	URI string
	IV  []byte // nil: derive from the media sequence number
}

// Playlist is a parsed m3u8: either a master (Variants) or media playlist.
type Playlist struct {
	Variants []Variant
	Init     string // #EXT-X-MAP URI (fMP4)
	Segments []Segment
}

// ParsePlaylist parses an m3u8 body, resolving URIs against base.
func ParsePlaylist(body string, base *url.URL) (*Playlist, error) {
	if !strings.HasPrefix(strings.TrimSpace(body), "#EXTM3U") {
		return nil, errors.New("not an m3u8 playlist")
	}
	resolve := func(s string) (string, error) {
		u, err := base.Parse(strings.TrimSpace(s))
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}

	p := &Playlist{}
	var (
		pendingVariant *Variant
		key            *Key
		seq            uint64
	)
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			attrs := parseAttrs(line[len("#EXT-X-STREAM-INF:"):])
			v := Variant{}
			v.Bandwidth, _ = strconv.Atoi(attrs["BANDWIDTH"])
			if res := attrs["RESOLUTION"]; res != "" {
				if _, h, ok := strings.Cut(res, "x"); ok {
					v.Height, _ = strconv.Atoi(h)
				}
			}
			pendingVariant = &v
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.ParseUint(line[len("#EXT-X-MEDIA-SEQUENCE:"):], 10, 64)
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			attrs := parseAttrs(line[len("#EXT-X-KEY:"):])
			switch attrs["METHOD"] {
			case "NONE":
				key = nil
			case "AES-128":
				uri, err := resolve(attrs["URI"])
				if err != nil {
					return nil, err
				}
				key = &Key{URI: uri}
				if iv := attrs["IV"]; iv != "" {
					b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(iv, "0x"), "0X"))
					if err != nil || len(b) != 16 {
						return nil, fmt.Errorf("bad IV %q", iv)
					}
					key.IV = b
				}
			default:
				return nil, fmt.Errorf("unsupported HLS encryption %q (DRM?)", attrs["METHOD"])
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			uri, err := resolve(parseAttrs(line[len("#EXT-X-MAP:"):])["URI"])
			if err != nil {
				return nil, err
			}
			p.Init = uri
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE"):
			return nil, errors.New("HLS byte-range segments are not supported")
		case strings.HasPrefix(line, "#"):
		default:
			uri, err := resolve(line)
			if err != nil {
				return nil, err
			}
			if pendingVariant != nil {
				pendingVariant.URL = uri
				p.Variants = append(p.Variants, *pendingVariant)
				pendingVariant = nil
			} else {
				p.Segments = append(p.Segments, Segment{URL: uri, Key: key, Seq: seq})
				seq++
			}
		}
	}
	return p, sc.Err()
}

// parseAttrs parses KEY=VALUE,KEY="quoted, value" lists.
func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for s != "" {
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		k = strings.TrimSpace(k)
		var v string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				v, s = rest[1:], ""
			} else {
				v, s = rest[1:end+1], strings.TrimPrefix(rest[end+2:], ",")
			}
		} else {
			v, s, _ = strings.Cut(rest, ",")
		}
		out[k] = v
	}
	return out
}

// BestVariant picks the tallest variant no taller than maxHeight (0 = any),
// breaking ties by bandwidth.
func BestVariant(vs []Variant, maxHeight int) (Variant, bool) {
	var best Variant
	found := false
	for _, v := range vs {
		if maxHeight > 0 && v.Height > maxHeight {
			continue
		}
		if !found || v.Height > best.Height || (v.Height == best.Height && v.Bandwidth > best.Bandwidth) {
			best, found = v, true
		}
	}
	if !found && len(vs) > 0 {
		best, found = vs[0], true
		for _, v := range vs {
			if v.Height < best.Height {
				best = v
			}
		}
	}
	return best, found
}

// HLS downloads the stream at playlistURL into dst. Segments are written in
// order to dst+".part"; a ".part.seg" file records progress for resume.
// If ffmpeg is installed the result is remuxed (no re-encode) to dst, which
// should end in .mp4; otherwise the raw stream is saved with a .ts extension.
// It returns the path actually written.
func HLS(ctx context.Context, c Getter, playlistURL, dst string, maxHeight int, h http.Header, progress Progress) (string, error) {
	pl, err := fetchPlaylist(ctx, c, playlistURL, h)
	if err != nil {
		return "", err
	}
	if len(pl.Variants) > 0 {
		v, _ := BestVariant(pl.Variants, maxHeight)
		if pl, err = fetchPlaylist(ctx, c, v.URL, h); err != nil {
			return "", err
		}
	}
	if len(pl.Segments) == 0 {
		return "", errors.New("playlist has no segments")
	}

	part := dst + ".part"
	state := part + ".seg"
	doneSegs, doneBytes := readState(state)
	if doneSegs > len(pl.Segments) {
		doneSegs, doneBytes = 0, 0 // playlist changed since last run
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Truncate(doneBytes); err != nil {
		return "", err
	}
	if _, err := f.Seek(doneBytes, io.SeekStart); err != nil {
		return "", err
	}

	if doneSegs == 0 && pl.Init != "" {
		b, err := getBytes(ctx, c, pl.Init, h)
		if err != nil {
			return "", fmt.Errorf("init segment: %w", err)
		}
		if _, err := f.Write(b); err != nil {
			return "", err
		}
		doneBytes += int64(len(b))
		writeState(state, 0, doneBytes)
	}

	keys := &keyCache{m: map[string][]byte{}}
	total := len(pl.Segments)
	err = fetchOrdered(ctx, pl.Segments[doneSegs:], 4,
		func(s Segment) ([]byte, error) {
			b, err := getBytesRetry(ctx, c, s.URL, h)
			if err != nil || s.Key == nil {
				return b, err
			}
			return decrypt(ctx, c, h, keys, s, b)
		},
		func(i int, b []byte) error {
			if _, err := f.Write(b); err != nil {
				return err
			}
			doneBytes += int64(len(b))
			doneSegs++
			writeState(state, doneSegs, doneBytes)
			if progress != nil {
				// Report segments as "bytes" so callers can show a fraction.
				progress(int64(doneSegs), int64(total))
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	os.Remove(state)

	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-loglevel", "error", "-y",
			"-f", inputFormat(pl), "-i", part, "-c", "copy", "-movflags", "+faststart", dst)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("ffmpeg remux: %v: %s", err, bytes.TrimSpace(out))
		}
		os.Remove(part)
		return dst, nil
	}
	ext := ".ts"
	if pl.Init != "" {
		ext = ".mp4"
	}
	out := strings.TrimSuffix(dst, ".mp4") + ext
	return out, os.Rename(part, out)
}

func inputFormat(pl *Playlist) string {
	if pl.Init != "" {
		return "mp4"
	}
	return "mpegts"
}

// fetchOrdered downloads items with n workers and hands results to write in
// the original order.
func fetchOrdered(ctx context.Context, segs []Segment, n int, get func(Segment) ([]byte, error), write func(int, []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		b   []byte
		err error
	}
	slots := make([]chan result, len(segs))
	for i := range slots {
		slots[i] = make(chan result, 1)
	}
	sem := make(chan struct{}, n)
	go func() {
		for i, s := range segs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			go func(i int, s Segment) {
				b, err := get(s)
				slots[i] <- result{b, err}
			}(i, s)
		}
	}()
	for i := range segs {
		var r result
		select {
		case r = <-slots[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
		<-sem
		if r.err != nil {
			return fmt.Errorf("segment %d: %w", i, r.err)
		}
		if err := write(i, r.b); err != nil {
			return err
		}
	}
	return nil
}

type keyCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func decrypt(ctx context.Context, c Getter, h http.Header, cache *keyCache, s Segment, data []byte) ([]byte, error) {
	cache.mu.Lock()
	k, ok := cache.m[s.Key.URI]
	cache.mu.Unlock()
	if !ok {
		var err error
		if k, err = getBytesRetry(ctx, c, s.Key.URI, h); err != nil {
			return nil, fmt.Errorf("key: %w", err)
		}
		if len(k) != 16 {
			return nil, fmt.Errorf("key is %d bytes, want 16", len(k))
		}
		cache.mu.Lock()
		cache.m[s.Key.URI] = k
		cache.mu.Unlock()
	}
	iv := s.Key.IV
	if iv == nil {
		iv = make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], s.Seq)
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, errors.New("encrypted segment is not block aligned")
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	if n := len(out); n > 0 {
		pad := int(out[n-1])
		if pad > 0 && pad <= aes.BlockSize && pad <= n {
			out = out[:n-pad]
		}
	}
	return out, nil
}

func fetchPlaylist(ctx context.Context, c Getter, u string, h http.Header) (*Playlist, error) {
	b, err := getBytesRetry(ctx, c, u, h)
	if err != nil {
		return nil, fmt.Errorf("playlist: %w", err)
	}
	base, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	return ParsePlaylist(string(b), base)
}

func getBytesRetry(ctx context.Context, c Getter, u string, h http.Header) ([]byte, error) {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			if e := sleep(ctx, time.Duration(attempt)*time.Second); e != nil {
				return nil, e
			}
		}
		var b []byte
		if b, err = getBytes(ctx, c, u, h); err == nil || errors.Is(err, ErrWrongIP) {
			return b, err
		}
	}
	return nil, err
}

func getBytes(ctx context.Context, c Getter, u string, h http.Header) ([]byte, error) {
	g := newStallGuard(ctx)
	defer g.close()
	req, err := http.NewRequestWithContext(g.ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, g.err(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == StatusWrongIP {
		return nil, ErrWrongIP
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(g.reader(resp.Body), 256<<20))
	return b, g.err(err)
}

func readState(path string) (int, int64) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	var segs int
	var n int64
	if _, err := fmt.Sscan(string(b), &segs, &n); err != nil {
		return 0, 0
	}
	return segs, n
}

func writeState(path string, segs int, n int64) {
	os.WriteFile(path, []byte(fmt.Sprintf("%d %d\n", segs, n)), 0o600)
}

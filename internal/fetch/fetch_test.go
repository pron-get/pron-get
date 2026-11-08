package fetch

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileResume(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 10000)
	var sawRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://ref/" {
			http.Error(w, "no referer", http.StatusPreconditionFailed)
			return
		}
		sawRange = r.Header.Get("Range")
		http.ServeContent(w, r, "v.mp4", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "v.mp4")
	os.WriteFile(dst+".part", data[:4321], 0o600) // pretend an earlier run died
	h := http.Header{"Referer": {"https://ref/"}}
	if err := File(context.Background(), srv.Client(), srv.URL, dst, h, nil); err != nil {
		t.Fatal(err)
	}
	if sawRange != "bytes=4321-" {
		t.Errorf("Range = %q", sawRange)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: %d bytes", len(got))
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Error(".part left behind")
	}
}

func TestFileExpiredURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "expired", http.StatusForbidden)
	}))
	defer srv.Close()
	err := File(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "x"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePlaylist(t *testing.T) {
	base, _ := url.Parse("https://cdn/hls/v/master.m3u8?tok=1")
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480,CODECS="avc1.4d401f,mp4a.40.2"
480/index.m3u8?tok=1
#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1920x1080
https://other/1080.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
720/index.m3u8`
	p, err := ParsePlaylist(master, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Variants) != 3 || p.Variants[0].URL != "https://cdn/hls/v/480/index.m3u8?tok=1" || p.Variants[0].Height != 480 {
		t.Fatalf("variants %+v", p.Variants)
	}
	if v, _ := BestVariant(p.Variants, 720); v.Height != 720 {
		t.Errorf("best<=720 = %+v", v)
	}
	if v, _ := BestVariant(p.Variants, 0); v.Height != 1080 {
		t.Errorf("best = %+v", v)
	}
	if v, _ := BestVariant(p.Variants, 240); v.Height != 480 {
		t.Errorf("best<=240 = %+v, want smallest", v)
	}

	media := `#EXTM3U
#EXT-X-MEDIA-SEQUENCE:7
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
#EXTINF:4.0,
seg7.ts
#EXT-X-KEY:METHOD=NONE
#EXTINF:4.0,
seg8.ts
#EXT-X-ENDLIST`
	p, err = ParsePlaylist(media, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Segments) != 2 || p.Segments[0].Key == nil || p.Segments[0].Seq != 7 || p.Segments[1].Key != nil {
		t.Fatalf("segments %+v", p.Segments)
	}
	if _, err := ParsePlaylist("#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://x\"\n", base); err == nil {
		t.Error("SAMPLE-AES should be refused")
	}
}

// hlsServer serves a master playlist, a media playlist of n AES-128
// encrypted segments, and the key.
func hlsServer(t *testing.T, segs [][]byte, fail map[string]int) *httptest.Server {
	key := []byte("0123456789abcdef")
	var failMu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=640x360\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2,RESOLUTION=1280x720\nhigh.m3u8\n")
	})
	mux.HandleFunc("/high.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\n")
		for i := range segs {
			fmt.Fprintf(w, "#EXTINF:1,\nseg%d.ts\n", i)
		}
		fmt.Fprint(w, "#EXT-X-ENDLIST\n")
	})
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) { w.Write(key) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var i int
		if _, err := fmt.Sscanf(r.URL.Path, "/seg%d.ts", &i); err != nil || i >= len(segs) {
			http.NotFound(w, r)
			return
		}
		failMu.Lock()
		flaky := fail[r.URL.Path] > 0
		if flaky {
			fail[r.URL.Path]--
		}
		failMu.Unlock()
		if flaky {
			http.Error(w, "flaky", http.StatusServiceUnavailable)
			return
		}
		w.Write(encrypt(key, uint64(i), segs[i]))
	})
	return httptest.NewServer(mux)
}

func encrypt(key []byte, seq uint64, plain []byte) []byte {
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := make([]byte, 16)
	binary.BigEndian.PutUint64(iv[8:], seq)
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return out
}

func TestHLSDecryptsInOrder(t *testing.T) {
	t.Setenv("PATH", "") // no ffmpeg: keep the raw stream
	var segs [][]byte
	var want []byte
	for i := 0; i < 20; i++ {
		s := []byte(strings.Repeat(fmt.Sprintf("segment-%02d|", i), i+1))
		segs = append(segs, s)
		want = append(want, s...)
	}
	srv := hlsServer(t, segs, map[string]int{"/seg3.ts": 2})
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "v.mp4")
	var last, total int64
	out, err := HLS(context.Background(), srv.Client(), srv.URL+"/master.m3u8", dst, 0, nil,
		func(d, n int64) { last, total = d, n })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, ".ts") {
		t.Errorf("out = %s", out)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, want) {
		t.Errorf("content mismatch:\n got %q\nwant %q", got[:min(80, len(got))], want[:80])
	}
	if last != 20 || total != 20 {
		t.Errorf("progress %d/%d", last, total)
	}
}

func TestHLSResume(t *testing.T) {
	t.Setenv("PATH", "")
	segs := [][]byte{[]byte("aaaa"), []byte("bbbb"), []byte("cccc")}
	srv := hlsServer(t, segs, nil)
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "v.mp4")
	// An earlier run wrote segment 0 plus half of segment 1.
	os.WriteFile(dst+".part", []byte("aaaabb"), 0o600)
	writeState(dst+".part.seg", 1, 4)
	out, err := HLS(context.Background(), srv.Client(), srv.URL+"/high.m3u8", dst, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "aaaabbbbcccc" {
		t.Errorf("got %q", got)
	}
}

func TestHLSRemuxWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	// Make a tiny real MPEG-TS clip and serve it as two segments.
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.ts")
	cmd := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=10:duration=1",
		"-c:v", "mpeg2video", "-f", "mpegts", clip)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot make test clip: %v %s", err, out)
	}
	b, _ := os.ReadFile(clip)
	cut := (len(b) / 2 / 188) * 188 // TS packets are 188 bytes
	srv := hlsServer(t, [][]byte{b[:cut], b[cut:]}, nil)
	defer srv.Close()

	dst := filepath.Join(dir, "v.mp4")
	out, err := HLS(context.Background(), srv.Client(), srv.URL+"/high.m3u8", dst, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != dst {
		t.Errorf("out = %s", out)
	}
	if fi, err := os.Stat(dst); err != nil || fi.Size() == 0 {
		t.Errorf("mp4 missing: %v", err)
	}
}

func TestFileWrongIP(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(StatusWrongIP)
	}))
	defer srv.Close()
	err := File(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "x"), nil, nil)
	if !errors.Is(err, ErrWrongIP) || calls != 1 {
		t.Fatalf("err = %v after %d calls; want ErrWrongIP without retrying", err, calls)
	}
}

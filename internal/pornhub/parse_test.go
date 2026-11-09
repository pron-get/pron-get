package pornhub

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestParseID(t *testing.T) {
	cases := map[string]string{
		"6a0123456789a": "6a0123456789a",
		"https://www.pornhub.com/view_video.php?viewkey=ph5af5fef7c2aa7": "ph5af5fef7c2aa7",
		"https://de.pornhub.com/view_video.php?pkey=1&viewkey=648719015": "648719015",
		"https://www.pornhub.com/embed/abc123def":                        "abc123def",
		"../../etc/passwd": "",
		"https://www.pornhub.com/view_video.php?viewkey=../x": "",
		"UPPER123": "",
	}
	for in, want := range cases {
		got, ok := ParseID(in)
		if got != want || ok != (want != "") {
			t.Errorf("ParseID(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

const videoPage = `<html><head>
<meta name="twitter:title" content="A &amp; B test video">
<script type="application/ld+json">{"author" : "Some Channel"}</script>
</head><body><script>
var flashvars_123 = {"video_title":"fallback","image_url":"https://img/x.jpg","video_duration":"95",
 "mediaDefinitions":[
  {"videoUrl":"/video/get_media?s=abc","format":"mp4","quality":[]},
  {"videoUrl":"https://cdn/hls/720P_4000K_1.mp4/master.m3u8","format":"hls","quality":"720"},
  {"videoUrl":"","format":"hls","quality":"480"}
 ]};
var other = 1;</script>
<div class="categoriesWrapper"><a data-label="category" class="item" href="/video?c=3">Amateur</a>
 <a data-label="category" class="item" href="/hd">HD Porn</a></div>
<div class="tagsWrapper"><p>Tags</p>
 <a data-label="tag" class="item isTag" href="/video/search?search=a"><span>cowgirl riding</span></a>
 <a data-label="tag" class="item isTag" href="/video/search?search=b"><span>caf&eacute; </span></a></div>
</body></html>`

func TestParseVideoPage(t *testing.T) {
	v, defs, err := parseVideoPage(videoPage)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "A & B test video" || v.Uploader != "Some Channel" || v.Duration != 95 {
		t.Errorf("got %+v", v)
	}
	if fmt.Sprint(v.Categories) != "[Amateur HD Porn]" || fmt.Sprint(v.Tags) != "[cowgirl riding café]" {
		t.Errorf("categories %q, tags %q", v.Categories, v.Tags)
	}
	if len(defs) != 3 || defs[1].Quality != 720 || defs[0].Quality != 0 {
		t.Errorf("defs %+v", defs)
	}
}

func TestPageErrors(t *testing.T) {
	cases := []struct {
		body string
		want error
	}{
		{`<body onload="go()">`, ErrChallenge},
		{`<script>document.cookie="RNKEY=1"</script>`, ErrChallenge},
		{`<div class="geoBlocked">nope</div>`, ErrGeoBlocked},
		{`<div class="video-wrapper removed"><span>This video has been disabled</span></div>`, ErrNotFound},
		{`<p>fine</p>`, nil},
	}
	for _, c := range cases {
		if err := pageError(c.body); !errors.Is(err, c.want) {
			t.Errorf("pageError(%q) = %v, want %v", c.body, err, c.want)
		}
	}
	if _, _, err := parseVideoPage(`<h1>Please complete age verification</h1>`); !errors.Is(err, ErrAgeVerification) {
		t.Errorf("age verification page: %v", err)
	}
}

func TestFormats(t *testing.T) {
	fs := dedupeSort([]Format{
		makeFormat("https://cdn/480P_2000K_1.mp4", "mp4", 0),
		makeFormat("https://cdn/hls/1080P_4000K_1.mp4/master.m3u8", "hls", 1080),
		makeFormat("https://cdn/1080P_4000K_1.mp4", "mp4", 1080),
		makeFormat("https://cdn/480P_2000K_1.mp4", "mp4", 480),
	})
	if len(fs) != 3 {
		t.Fatalf("dedupe: %+v", fs)
	}
	if fs[0].Height != 1080 || fs[0].Kind != "mp4" || fs[1].Kind != "hls" || fs[2].Height != 480 {
		t.Errorf("order: %+v", fs)
	}
	if f, _ := Pick(fs, 720); f.Height != 480 {
		t.Errorf("Pick 720 = %+v", f)
	}
	if f, _ := Pick(fs, 100); f.Height != 480 {
		t.Errorf("Pick below all = %+v, want smallest", f)
	}
}

const searchPage = `<ul id="header-menu"><a href="/view_video.php?viewkey=menu00001" title="Menu link"></a></ul>
<div class="container"><ul>
<li class="pcVideoListItem" data-video-vkey="aaaaaa111">
  <a href="/view_video.php?viewkey=aaaaaa111" title="First &quot;one&quot;" class="thumb"></a>
  <var class="duration">12:56</var>
  <a href="/view_video.php?viewkey=aaaaaa111" title="First &quot;one&quot;">First</a>
</li>
<li class="pcVideoListItem" data-video-vkey="bbbbbb222">
  <a href="/view_video.php?viewkey=bbbbbb222" title="Second" class="thumb"></a>
  <var class="duration">1:02:03</var>
</li></ul></div>`

func TestParseSearch(t *testing.T) {
	rs := parseSearch(searchPage)
	if len(rs) != 2 {
		t.Fatalf("got %+v", rs)
	}
	if rs[0] != (Result{"aaaaaa111", `First "one"`, "12:56"}) || rs[1] != (Result{"bbbbbb222", "Second", "1:02:03"}) {
		t.Errorf("got %+v", rs)
	}
}

// PRON_GET_FIXTURES=dir checks the parsers against real saved pages.
func TestSavedPages(t *testing.T) {
	dir := os.Getenv("PRON_GET_FIXTURES")
	if dir == "" {
		t.Skip("set PRON_GET_FIXTURES to a directory with search.html and video.html")
	}
	b, err := os.ReadFile(dir + "/search.html")
	if err != nil {
		t.Fatal(err)
	}
	rs := parseSearch(string(b))
	missing := 0
	for _, r := range rs {
		if r.Duration == "" {
			missing++
		}
	}
	t.Logf("search: %d results, %d without duration, first %+v", len(rs), missing, rs[0])
	if b, err = os.ReadFile(dir + "/video.html"); err != nil {
		t.Fatal(err)
	}
	v, defs, err := parseVideoPage(string(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("video: %q by %q, %ds, %d media definitions", v.Title, v.Uploader, v.Duration, len(defs))
}

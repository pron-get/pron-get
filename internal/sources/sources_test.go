package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `# comment
https://www.pornhub.com/model/example-model   # trailing comment

  https://www.pornhub.com/playlist/123456789 limit=100
search oil   massage limit=0
search
https://evil.example/model/x
https://www.pornhub.com/model/x limit=-1
https://www.pornhub.com/model/x colour=red
`
	ss, err := Parse(strings.NewReader(in))
	if len(ss) != 3 {
		t.Fatalf("got %d sources: %+v", len(ss), ss)
	}
	want := []Source{
		{Line: 2, Raw: "https://www.pornhub.com/model/example-model", Path: "/model/example-model/videos", Limit: DefaultLimit},
		{Line: 4, Raw: "https://www.pornhub.com/playlist/123456789 limit=100", Path: "/playlist/123456789", Limit: 100},
		{Line: 5, Raw: "search oil massage limit=0", Path: "/video/search?search=oil+massage", Limit: 0},
	}
	for i := range want {
		if ss[i] != want[i] {
			t.Errorf("source %d = %+v, want %+v", i, ss[i], want[i])
		}
	}
	if err == nil {
		t.Fatal("expected errors for bad lines")
	}
	for _, l := range []string{"line 6", "line 7", "line 8", "line 9"} {
		if !strings.Contains(err.Error(), l) {
			t.Errorf("error missing %s: %v", l, err)
		}
	}
}

func TestLoadCreatesTemplateAndAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "sources.list")
	ss, err := Load(path)
	if err != nil || len(ss) != 0 {
		t.Fatalf("Load new = %v, %v", ss, err)
	}
	if b, _ := os.ReadFile(path); string(b) != Template {
		t.Fatal("template not written")
	}
	if ss, err := Load(path); err != nil || len(ss) != 0 {
		t.Fatalf("template should parse to nothing: %v %v", ss, err)
	}

	if _, err := Add(path, "https://www.pornhub.com/model/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(path, "https://www.pornhub.com/model/x/videos"); err == nil {
		t.Error("duplicate accepted")
	}
	if _, err := Add(path, "https://evil.example/"); err == nil {
		t.Error("bad source accepted")
	}
	ss, err = Load(path)
	if err != nil || len(ss) != 1 || ss[0].Path != "/model/x/videos" {
		t.Fatalf("after Add: %+v %v", ss, err)
	}
}

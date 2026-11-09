package ukcpa

import "testing"

func TestCheck(t *testing.T) {
	cases := []struct {
		title      string
		tags, cats []string
		want       string // offence, "" for no match
	}{
		{"Morning yoga session", []string{"cowgirl riding"}, []string{"Amateur"}, ""},
		{"Rough and CHOKING", nil, nil, "strangulation or suffocation"},
		{"Massage", []string{"breath-play"}, nil, "strangulation or suffocation"},
		// whole words only
		{"Leather choker and boots", []string{"deepthroat"}, nil, ""},
		// step on its own is not covered
		{"Stepmom helps out", []string{"step fantasy"}, []string{"MILF"}, ""},
		// step with a youth marker anywhere is
		{"Stepsister after class", nil, []string{"18-25", "Step Fantasy"}, "step or foster relatives portrayed as under 18"},
		{"Cosplay", []string{"school uniform"}, nil, "someone portrayed as under 16"},
	}
	for _, c := range cases {
		m, ok := Check(c.title, c.tags, c.cats)
		if m.Offence != c.want || ok != (c.want != "") {
			t.Errorf("Check(%q, %q, %q) = %+v, %v; want %q", c.title, c.tags, c.cats, m, ok, c.want)
		}
	}
}

func TestCheckReportsField(t *testing.T) {
	m, _ := Check("Plain title", []string{"smothering"}, nil)
	if m.Field != "tag" || m.Term != "smothering" {
		t.Errorf("got %+v", m)
	}
}

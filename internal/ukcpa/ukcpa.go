// Package ukcpa flags videos whose title, tags or categories suggest they
// fall under the pornography offences of the Crime and Policing Act 2026
// (England and Wales, in force from 6 November 2026).
//
// The offences turn on what a reasonable person would think on seeing the
// images, so this is a keyword filter, not a legal test: it misses some
// videos and flags others that are lawful.
package ukcpa

import (
	"strings"
	"unicode"
)

// Match says why a video was flagged.
type Match struct {
	Offence string // which of the Act's categories
	Term    string // the words that matched
	Field   string // "title", "tag" or "category"
}

type rule struct {
	offence string
	terms   []string
	// If set, one of these must also appear somewhere in the video's text.
	alongside []string
}

var rules = []rule{
	{
		offence: "strangulation or suffocation",
		terms: []string{
			"choke", "choked", "choking", "chokes",
			"strangle", "strangled", "strangling", "strangulation",
			"suffocate", "suffocated", "suffocating", "suffocation",
			"smother", "smothered", "smothering",
			"breath play", "breathplay", "asphyxia", "asphyxiation",
			"hand on throat", "hands on throat", "hand around throat",
			"throat grab", "throat grabbing", "neck grab",
		},
	},
	{
		offence: "sex between relatives",
		terms: []string{
			"incest", "family taboo", "taboo family",
			"father daughter", "dad daughter", "daddy daughter", "daughter dad",
			"mother son", "mom son", "mum son", "son mom", "son mum",
			"brother sister", "sister brother", "real sister", "real brother",
		},
	},
	{
		// Step and foster relationships count only where one person is, or
		// is pretending to be, under 18.
		offence: "step or foster relatives portrayed as under 18",
		terms: []string{
			"stepdaughter", "step daughter", "stepsister", "step sister", "stepsis", "step sis",
			"stepbrother", "step brother", "stepbro", "step bro", "stepson", "step son",
			"stepdad", "step dad", "stepfather", "step father",
			"stepmom", "step mom", "stepmum", "step mum", "stepmother", "step mother",
			"step fantasy", "step family", "foster",
		},
		alongside: []string{
			"teen", "teens", "teenager", "18 25", "18 year", "18 years", "18yo", "18 yo",
			"barely legal", "young", "school", "schoolgirl", "school girl",
		},
	},
	{
		offence: "someone portrayed as under 16",
		terms: []string{
			"underage", "under age", "jailbait", "loli", "lolita", "shota",
			"preteen", "pre teen", "child", "little girl", "little boy",
			"schoolgirl", "school girl", "schoolboy", "school uniform",
			"middle school", "junior high", "age play", "ageplay",
		},
	},
}

// Check returns the first rule the video matches, if any.
func Check(title string, tags, categories []string) (Match, bool) {
	type field struct{ name, text string }
	fields := []field{{"title", norm(title)}}
	for _, t := range tags {
		fields = append(fields, field{"tag", norm(t)})
	}
	for _, c := range categories {
		fields = append(fields, field{"category", norm(c)})
	}
	var all strings.Builder
	for _, f := range fields {
		all.WriteString(f.text)
	}

	for _, r := range rules {
		if len(r.alongside) > 0 && !containsAny(all.String(), r.alongside) {
			continue
		}
		for _, f := range fields {
			for _, t := range r.terms {
				if strings.Contains(f.text, " "+t+" ") {
					return Match{Offence: r.offence, Term: t, Field: f.name}, true
				}
			}
		}
	}
	return Match{}, false
}

func containsAny(text string, terms []string) bool {
	for _, t := range terms {
		if strings.Contains(text, " "+t+" ") {
			return true
		}
	}
	return false
}

// norm lowercases s and turns everything but letters and digits into single
// spaces, padded at both ends, so terms match whole words only.
func norm(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	if !space {
		b.WriteByte(' ')
	}
	return b.String()
}

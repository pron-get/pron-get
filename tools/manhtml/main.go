// manhtml renders man/pron-get.1 as an HTML page that looks like the manual
// open in an xterm. It runs mandoc(1) for the text, so the page matches what
// `man pron-get` prints, bold and underline included.
//
//	go run ./tools/manhtml -version 0.7.4 > docs/pron-get.1.html
//
// -fragment leaves out the <!doctype>/<html>/<head> wrapper, for hosts that
// add their own.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"strings"
)

func main() {
	src := flag.String("man", "man/pron-get.1", "mdoc source")
	version := flag.String("version", "dev", "pron-get version shown in the footer and caption")
	fragment := flag.Bool("fragment", false, "omit the document wrapper")
	flag.Parse()

	// -I os= keeps the build machine's OS name and version out of the footer.
	out, err := exec.Command("mandoc", "-T", "ascii", "-O", "width=78",
		"-I", "os=pron-get "+*version, *src).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "manhtml: mandoc:", err)
		os.Exit(1)
	}
	body := strings.NewReplacer(
		"@@BODY@@", overstrikeToHTML(string(out)),
		"@@VERSION@@", html.EscapeString(*version),
	).Replace(bodyTemplate)
	// Hosts that add their own wrapper (Claude artifacts) read this tag as a
	// promise that the side gutter clears the phone's safe area.
	page := strings.Replace(headTemplate, "</title>\n",
		"</title>\n<meta name=\"claude:viewport-fit-sides\" content=\"cover\">\n", 1) + body
	if !*fragment {
		page = "<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n" +
			"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1,viewport-fit=cover\">\n" +
			headTemplate + "</head>\n<body>\n" + body + "</body>\n</html>\n"
	}
	os.Stdout.WriteString(page)
}

// overstrikeToHTML turns nroff overstrike ("c\bc" bold, "_\bc" underline)
// into <span class="b"> and <span class="u">, escaping everything else.
func overstrikeToHTML(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var b bytes.Buffer
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		r := []rune(line)
		cur := ""
		for j := 0; j < len(r); {
			ch, style := r[j], ""
			if j+2 < len(r) && r[j+1] == '\b' {
				ch = r[j+2]
				style = "b"
				if r[j] == '_' && ch != '_' {
					style = "u"
				}
				j += 3
				for j+1 < len(r) && r[j] == '\b' { // bold underline: c\bc\bc
					j += 2
				}
			} else {
				j++
			}
			if style != cur {
				if cur != "" {
					b.WriteString("</span>")
				}
				if style != "" {
					fmt.Fprintf(&b, `<span class="%s">`, style)
				}
				cur = style
			}
			b.WriteString(html.EscapeString(string(ch)))
		}
		if cur != "" {
			b.WriteString("</span>")
		}
	}
	return b.String()
}

const headTemplate = `<title>pron-get(1)</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:ital,wght@0,400;0,700;1,400&family=IBM+Plex+Sans+Condensed:wght@600&display=swap">
<style>
/* One xterm-style window on an X11 root-weave desktop; the text is mandoc's
   own output, line for line. Single-theme on purpose: it is a picture of a
   terminal. */
:root {
  --desk-a: #9a9893;      /* root weave, light thread */
  --desk-b: #85837e;      /* root weave, dark thread */
  --chrome: #d4d1ca;      /* window frame */
  --chrome-hi: #f2f0ea;   /* bevel highlight */
  --chrome-lo: #807d76;   /* bevel shadow */
  --title: #3d4a5c;       /* focused title bar */
  --title-ink: #f4f2ec;
  --paper: #fdfdfc;       /* terminal background */
  --ink: #141414;         /* terminal text */
  --muted: #3a3935;
  --mono: "IBM Plex Mono", Menlo, "DejaVu Sans Mono", Consolas, monospace;
  --ui: "IBM Plex Sans Condensed", "Helvetica Neue", Arial, sans-serif;
  color-scheme: light;
}
html { background: var(--desk-b); }
body {
  margin: 0;
  background-color: var(--desk-a);
  background-image:
    repeating-linear-gradient(45deg, var(--desk-b) 0 1px, transparent 1px 4px),
    repeating-linear-gradient(-45deg, var(--desk-b) 0 1px, transparent 1px 4px);
  color: var(--ink);
  min-height: 100vh;
  padding-block: 40px 48px;
  padding-left: max(16px, env(safe-area-inset-left, 0px));
  padding-right: max(16px, env(safe-area-inset-right, 0px));
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
}
.window {
  width: fit-content;
  max-width: 100%;
  min-width: 0;
  background: var(--chrome);
  border: 2px solid;
  border-color: var(--chrome-hi) var(--chrome-lo) var(--chrome-lo) var(--chrome-hi);
  padding: 4px;
  box-shadow: 6px 8px 0 rgba(30, 29, 26, .35);
  box-sizing: border-box;
}
.titlebar {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--title);
  color: var(--title-ink);
  font: 600 13px/1 var(--ui);
  letter-spacing: .02em;
  padding: 4px 4px 4px 8px;
  margin-bottom: 4px;
}
.titlebar .name { flex: 1; min-width: 0; text-align: center; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.btn {
  width: 16px; height: 14px; flex: none;
  background: var(--chrome);
  border: 2px solid;
  border-color: var(--chrome-hi) var(--chrome-lo) var(--chrome-lo) var(--chrome-hi);
  box-sizing: border-box;
  position: relative;
}
.btn.menu::after { content: ""; position: absolute; left: 2px; right: 2px; top: 3px; height: 2px; background: var(--muted); }
.btn.min::after  { content: ""; position: absolute; left: 4px; top: 3px; width: 4px; height: 4px; border: 1px solid var(--muted); }
.btn.max::after  { content: ""; position: absolute; left: 1px; top: 1px; right: 1px; bottom: 1px; border: 1px solid var(--muted); }
.screen {
  background: var(--paper);
  border: 2px solid;
  border-color: var(--chrome-lo) var(--chrome-hi) var(--chrome-hi) var(--chrome-lo);
  overflow-x: auto;
}
pre {
  margin: 0;
  padding: 10px 12px 0;
  font: 400 13px/1.38 var(--mono);
  color: var(--ink);
  tab-size: 8;
  width: max-content;
}
pre .b { font-weight: 700; }
pre .u { text-decoration: underline; text-decoration-thickness: 1px; text-underline-offset: 2px; }
.status {
  display: block;
  margin: 6px 12px 10px;
  width: fit-content;
  background: var(--ink);
  color: var(--paper);
  font: 400 13px/1.38 var(--mono);
  white-space: pre;
}
.caption {
  margin: 0;
  font: 400 12px/1.4 var(--mono);
  color: var(--title-ink);
  background: rgba(40, 39, 36, .78);
  padding: 4px 10px;
  max-width: 100%;
  box-sizing: border-box;
  text-align: center;
}
.caption b { font-weight: 700; }
.caption a { color: inherit; text-underline-offset: 2px; }
.caption a:hover, .caption a:focus-visible { color: #fff; outline: 1px dotted currentColor; outline-offset: 2px; }
@media (max-width: 520px) {
  body { padding-block: 20px 28px; }
  pre, .status { font-size: 11.5px; }
}
</style>
`

const bodyTemplate = `<div class="window" role="figure" aria-label="The pron-get(1) manual page in a terminal">
  <div class="titlebar" aria-hidden="true">
    <span class="btn menu"></span>
    <span class="name">xterm — man pron-get</span>
    <span class="btn min"></span>
    <span class="btn max"></span>
  </div>
  <div class="screen">
<pre>
@@BODY@@
</pre>
<span class="status"> Manual page pron-get(1) line 1 (press h for help or q to quit)</span>
  </div>
</div>
<p class="caption"><a href="https://pron-get.github.io/pron-get/"><b>pron-get @@VERSION@@</b></a> · apt-get for porn, over Tor · <a href="https://github.com/pron-get/pron-get">github.com/pron-get/pron-get</a></p>
`

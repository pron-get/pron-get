# pron-get

An `apt-get` style downloader for porn, over Tor. A tribute to
[porn-get](HISTORY.md) (2001), rebuilt in Go.

```
$ pron-get search massage
Hit:1 tor 127.0.0.1:49685 exit 185.220.100.251
Reading Package Lists... Done
  6a0123456789a      22:41  ...
$ pron-get install -q 720 6a0123456789a
Get:1 6a0123456789a 720p/mp4 ...
   39% [17.0 MB/43.2 MB] 522.1 kB/s
Setting up 6a0123456789a (43.2 MB) ...
$ pron-get install -j 3 -limit 10 https://www.pornhub.com/model/example-model
Reading package list /model/example-model/videos... page 1, 10 packages Done
The following NEW packages will be installed:
  ...
Do you want to continue? [Y/n]
$ pron-get list
$ pron-get remove 6a0123456789a
$ pron-get moo
```

## Install

pron-get needs `tor` at run time. `ffmpeg` is optional: it turns HLS
downloads into `.mp4` files.

**Homebrew** (macOS, Linux):

```
brew install pron-get/tap/pron-get
```

**Binaries** for macOS, Linux and FreeBSD (amd64, arm64) are on the
[releases page](https://github.com/pron-get/pron-get/releases), with
`checksums.txt`. Unpack, put `pron-get` on your `PATH`, and copy
`man/pron-get.1` into a `man1` directory.

**Go**:

```
go install github.com/pron-get/pron-get/cmd/pron-get@latest
```

For `pron-cache`, link it to the same binary:
`ln -s "$(command -v pron-get)" /usr/local/bin/pron-cache`.

Read the manual without installing it: `man ./man/pron-get.1`, or open
[docs/pron-get.1.html](docs/pron-get.1.html) in a browser.

## Commands

| | |
|---|---|
| `search <words>` | search; `-page N` for more. `i` marks installed packages |
| `search <listing-url>` | list a model, pornstar, channel, user, playlist or category (`-limit`, default 100) |
| `show <id\|url>` | title, uploader, duration and available qualities |
| `install <id\|url>...` | download to `~/pr0n/<id>/`. `-q 720` caps quality, `-hls` prefers the stream |
| `install <listing-url>...` | bulk install from listings; `-search "words"` adds a search. `-limit N` per listing (default 3, newest first; 0 = all), `-j N` parallel downloads, `-y` skips the prompt |
| `remove <id>...` | delete packages (`-y` skips the prompt); `upgrade` won't reinstall them |
| `update` | fetch every source in `sources.list` into the package index |
| `upgrade` | install everything new in the index (`-limit`, `-j`, `-q`, `-y`) |
| `list` | installed packages; `-upgradable` for what `upgrade` would install |
| `add-source <url \| search words> [limit=N]` | append to `sources.list` |
| `edit-sources` | open `sources.list` in `$EDITOR` |
| `cache search\|show\|pkgnames\|stats` | query the index offline (also runs as `pron-cache`) |
| `torcheck` | show which Tor exit you'd be using |

Common flags: `-dir`, `-tor host:port`, `-tor-bin`, `-site`, `-onion`.

## sources.list, update, upgrade

Like the original, pron-get can follow sources. `~/.config/pron-get/sources.list`
(created with examples on first use) holds one listing per line:

```
https://www.pornhub.com/model/example-model
https://www.pornhub.com/playlist/123456789 limit=100
search oil massage limit=20
```

`limit=N` caps how many of the newest videos are indexed per source (default 40, `0` = all).

```
$ pron-get update
Hit:1 tor 127.0.0.1:50133 exit 85.11.167.137
Hit:2 /model/example-model/videos [5 packages]
Get:3 /video/search?search=oil+massage [3 packages]
Fetched 8 packages in 3s
Reading Package Lists... Done
Building dependency tree... Done
6 packages can be upgraded (3 new since last update). Run 'pron-get upgrade' to see them.
$ pron-get upgrade -j 3
```

- Each source is fetched on its own Tor circuit, so the site can't link your
  sources to each other.
- A source that fails keeps its old index entries (`W: Some index files failed...`).
- `remove` marks a package as removed, so `upgrade` skips it; `install <id>`
  brings it back.
- `upgrade` asks before it touches the network.
- The index lives in `~/Library/Caches/pron-get/index.json` (`-index` to change),
  and `pron-cache` / `pron-get cache` searches it offline:
  `ln -s "$(which pron-get)" /usr/local/bin/pron-cache`.

## Privacy model

- **Tor or nothing.** All traffic goes through a SOCKS5 proxy: a system tor
  (9050), Tor Browser (9150), or a private `tor` that pron-get starts and
  stops itself. Before any request to the site, check.torproject.org must
  confirm a Tor exit, or pron-get exits.
- **No DNS leaks.** Hostnames are sent to Tor unresolved; the private tor runs
  with `SafeSocks 1`, which refuses requests that would have needed local DNS.
  `HTTP(S)_PROXY` and the system torrc are ignored.
- **Unlinkable videos.** Each installed video uses a fresh circuit
  (`IsolateSOCKSAuth`), so the site sees each one from a different exit.
  Parallel jobs (`-j`) each get their own client and circuits.
- **Blends in.** Tor Browser's User-Agent; cookies in memory only, gone on exit.
- **Local.** Package dirs are created `0700`.

What it does *not* protect: anything on your disk (`~/pr0n` is plain files),
and the site still sees what is downloaded, just not by whom.

## Scope

Public videos only. pron-get reports and does not bypass: premium/paid
content, private or locked videos, geo-blocks, and age-verification walls.
The only cookie it sets is the click-through "I am 18+" disclaimer.

## Notes

- Media links are signed for the exit IP that resolved them (`ip=` in the
  URL) and throttled to ~500 kB/s per link. A CDN answer of HTTP 474 means
  the IP didn't match (some exits use several outbound IPs); pron-get then
  re-resolves on a new circuit and resumes. Use `-j` to download several
  videos at once; each gets its own 500 kB/s.
- The site's onion address (`-onion`) was offline when tested (Nov 2026).
- Interrupted downloads keep a `.part` file and resume on the next `install`.

## Development

```
go run ./tools/manhtml -version 0.7.0 > docs/pron-get.1.html   # after editing man/pron-get.1
go test -race ./...
PRON_GET_LIVE=1 go test ./internal/tor ./internal/pornhub -run Live -v   # real network
```

## License

GPL-2.0-or-later, like porn-get before it. See [LICENSE](LICENSE).

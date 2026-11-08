# History

pron-get is a modern take on **porn-get**, a joke-but-working bash tool from 2001
that copied the `apt-get` interface to download free adult image galleries.

## porn-get (2001–2003)

| | |
|---|---|
| Name | `porn-get` (+ `porn-cache`), part of the joke distro "Lesbian GNU/Linux" |
| Authors | `tarzeau` (original), `pl0pix` / Erik (`ew@code.de`, 0.5.x rewrite) |
| First version | 0.0.3, 28 Jun 2001 ("made porn-get just for the fun of it", 27 Jun) |
| Last version | 0.5.1, files dated 2003-06-04 |
| License | GPL v2 or later |
| Language | Bash, plus two small Perl helpers |
| Dependencies | `bash`, `lynx`, `wget` (>= 1.6) |
| Home (dead) | `http://www.linuks.mine.nu/porn-get` |
| Archive | SourceForge project `lesbianlinux`, uploaded 2008-11-22 by `mjd39` |

All recovered versions (0.1.2, 0.2.5, 0.4.0, 0.5.1) are in [history/](history/README.md).

| Version | Date | Highlights |
|---|---|---|
| 0.0.3–0.0.9 | Jun–Jul 2001 | first releases, background downloads, `remove` |
| 0.1.2 | Jul 2001 | `--referer`, ~1000-package index, +9000 more in Aug |
| 0.2.5 | Aug 2001 | https, `porn-get share` + `porn-give.cgi` |
| 0.4.0 | Aug 2001 | makefile, manpage, Super Cow Powers |
| 0.5.1 | Jun 2003 | pl0pix rewrite: `sources.list` scraping, Perl link cleaners, rc file |

The opening comment in the script explains why it exists:

> netscape always crashes when i surf sex and love education homepages
> therefore i made porn-get (werks almost like debian's apt-get)

### Commands

```
porn-get update              # rebuild the package index (porn.conf) from sources.list
porn-get upgrade             # append to the index without wiping it
porn-get install <pattern>   # download every gallery whose name matches
porn-get remove <package>    # delete a downloaded gallery (asks y/N, apt style)
porn-get share <name> <url> <ext>   # submit a gallery (listed in readme)
porn-cache search <pattern>
porn-cache pkgnames
porn-cache stats
porn-get moo                 # "This porn has Super Cow Powers."
```

### How it worked

1. **`sources.list`** listed "mirrors": free gallery/link-list sites (TGPs such as
   thehun.org, thumbzilla.com, tommys-bookmarks.com), with optional file extension
   and category paths. Format: `uri [extension category [category2] ...]`.
2. **`update`** ran `lynx -dump` on each source page. `liblinkextract.pl` pulled out
   the links. `liblinkclean.pl` removed per-site CGI redirect/tracking wrappers
   (`out.php?url=`, `c.cgi?url=`, sextracker counters).
3. The result was **`porn.conf`**, the package index: one line per gallery,
   `pkgname url fileext`. Duplicates were removed with `sort | uniq -f 1`.
4. **`install`** grepped `porn.conf`, made a directory per "package" under
   `~/pr0n/`, ran `lynx -dump` on the gallery page, and used `wget -c` with a
   `--referer` header to fetch every link that matched the file extension.
5. **Cleanup:** files under `BANNERSIZE` (7000 bytes) were deleted as banners or
   thumbnails. Leftover `.htm`/`.php` files were removed. Empty galleries were deleted
   ("E: gpkg fucked ;)").
6. Config lived in `porn-getrc` (global, `/etc`, or `~/.porn-getrc`): `USERDIR`,
   `BANNERSIZE`, `LINKLIST`, `DEBUG`.

The output copied apt's wording on purpose: `Hit: <site> Packages [n]`,
`Reading Package Lists... Done`, `E: Opening ... - ifstream::ifstream (2 No such
file or directory)`.

### TODO list from the original readme (never finished)

- trap Ctrl-C
- remove dead packages (dead links)
- list package contents and sizes
- "dependencies" / task packages (installing `lesbian*` offers related packages)
- a per-file md5 database (`<md5sum> <url1> [<url2> ...]`, a TODO comment in `getfiles`)

### Reception

The readme collects IRC quotes from OPN channels (#debian, #e, #suse, #windowmaker)
and links from kuro5hin (Aug 2001), NTK (2001-08-24), linuxnewbie.org, and the
debian-security list (Aug 2001). Typical quote:

> "now thanks to porn-get I can look like I am hacking scripts when I am actually getting porn"

### Related tools of the era

- **gnaughty**: a utility that auto-downloaded from an adult link directory
- **Porn Toolkit**: a collection of scripts for downloading free videos

## Why it stopped working

- Every source in `sources.list` was a 2001–2003 TGP site; most are gone or changed.
- It scraped with `lynx -dump`, which has no JavaScript, cookies, or age-gate handling.
- Galleries moved from static image pages to streaming video (HLS/DASH).
- It had no privacy layer: every request went straight from your IP, with the
  User-Agent `www.linuks.mine.nu/porn-get-<version>`.

## What pron-get keeps / changes

| Keep | Change |
|---|---|
| apt-style verbs: `update`, `install`, `remove`, `search`, `moo` | Go single binary instead of bash + lynx + wget + perl |
| `sources.list` → `update` → index → install | listing sources (models, channels, playlists, searches) with per-source limits; `upgrade` installs what is new |
| per-package directories, resume (`wget -c`) | Concurrent downloads, checksums (the md5 db TODO from 2003) |
| banner/thumbnail filtering | Real media-type checks instead of a file-size guess |
| | All traffic through Tor (SOCKS5), with per-source circuit isolation |

## Sources

- SourceForge project: https://sourceforge.net/projects/lesbianlinux/
- Release file: https://sourceforge.net/projects/lesbianlinux/files/lesbianlinux/porn-get_0.5.1/porn-get_0.5.1.tar.gz
  (the archive is truncated: gzip reports "truncated input". `porn.conf` and the
  section-5 manpages are missing, but every file listed below extracted intact.
  Its `changelog` is actually a pre-release tarball; see history/README.md.)
- Wayback Machine captures of `linuks.mine.nu/porn-get` (2001–2002): homepage,
  0.2.5 and 0.4.0 tarballs, loose 0.1.2 files
- Files read from the tarball: `readme`, `porn-get`, `porn-cache`, `porn-getrc`,
  `sources.list`, `makefile`, `porn.1`, `liblinkclean.pl`, `liblinkextract.pl`

# porn-get archive

Every surviving version of the original porn-get (2001–2003) that could be found.
See [../HISTORY.md](../HISTORY.md) for what it was and how it worked.

These are historical artifacts: read them, don't run them. The scripts `rm -rf`
paths built from user input, and every site in their source lists is long dead.

## Versions

| Version | Date | Folder | Source | What's there |
|---|---|---|---|---|
| 0.1.2 | 2001-07-04 | [0.1.2/](0.1.2/) | Wayback, loose files captured 2001-07-22 | `porn-get`, `porn-cache`, `readme`, `changelog`. No tarball was archived. The 2001 `porn.conf` package index (a list of link-site URLs) is deliberately not included. |
| 0.2.5 | 2001-08-18 | [0.2.5/](0.2.5/) | Wayback, `old/porn-get-0.2.5.tar.bz2` | Original tarball + extracted `src/` |
| 0.4.0 | 2001-08-25 | [0.4.0/](0.4.0/) | Wayback, `porn-get-0.4.0.tar.bz2` | Original tarball + extracted `src/`. Adds `makefile` and `porn.1`. |
| 0.5.1 (pre) | 2003-06-04 | [0.5.1/pre-release/](0.5.1/pre-release/) | Found inside the 0.5.1 tarball | Extracted `src/` (redacted) of an earlier 0.5.1 tarball that ended up in the release where `changelog` should be. Differs from the final build only in `porn-get` `update` (no `-r` check before `rm $conf`). |
| 0.5.1 | 2003-06-04 | [0.5.1/](0.5.1/) | SourceForge `lesbianlinux`, uploaded 2008-11-22 | Extracted `src/` (redacted). Rewrite by pl0pix/Erik: `porn-getrc`, `sources.list`, Perl link extract/clean helpers, banner filter. |

### Other files

- [site/index.html](site/index.html): the Lesbian GNU/Linux / porn-get homepage, captured 2002-02-01
- [snapshots/](snapshots/): loose-file captures that differ from the tarball copies
  - `2001-08-20_readme`, `2001-08-20_changelog`: the 0.2.5-era web copies (changelog lacks the "(unstable)" note)
  - `2002-02-13_readme`: the 0.4.0-era web readme, with a Solaris/SPARC note and an extra IRC quote not in the tarball

## Versions known but not recovered

From the changelogs: 0.0.3, 0.0.6, 0.0.7, 0.0.9 (Jun–Jul 2001); 0.1.3; 0.2.3; 0.3.2,
0.3.7, 0.3.8 (Aug 2001); anything between 0.4.0 and 0.5.1. Also listed on the homepage,
but never captured: `porn-setup`, `porn-setup-merrychristmas`, `porn-get-windows.zip`,
`porn-give.cgi`, `porn2.conf`, the `.tar.gz` variants, and the
`lesbiangnulinux-2.2r4.iso` (hosted on vlaadworld.net).

## Version timeline (from the changelogs)

```
27/jun/2001 unreleased   made porn-get just for the fun of it
28/jun/2001 0.0.3        "works better that i thought it would"
01/jul/2001 0.0.6        downloads in background
02/jul/2001 0.0.7        wget/lynx check, porn-cache stats + pkgnames
02/jul/2001 0.0.9        porn-get remove, broken packages deleted
04/jul/2001 0.1.2        grep -i on extension, --referer
09/aug/2001 0.1.2        +~9000 packages (porn2.conf)
11/aug/2001 0.1.3        bugfixes, verbose remove
17/aug/2001 0.2.3        https support
18/aug/2001 0.2.5        porn-get share + porn-give.cgi
21/aug/2001 0.3.2        makefile, porn*.conf support
22/aug/2001 0.3.7        usage text, share bugfix
23/aug/2001 0.3.8        $what (symlink to corn-get...), .conf check
25/aug/2001 0.4.0        Super Cow Powers, porn.1 manpage
04/jun/2003 0.5.1        pl0pix rewrite (no changelog survived)
```

## Redactions

This archive is for publication, so a few inherited references are left out:

- `0.1.2/porn.conf`, the 2001 package index, is not included at all.
- In both 0.5.1 copies, `sources.list` loses one site and two category names,
  and `liblinkclean.pl` loses that site's link-cleaning block. Each spot is
  marked `removed for publication`.
- The 0.5.1 tarballs are not included, because they hold the unredacted
  files. The SourceForge link below still serves the original.

The 0.2.5 and 0.4.0 tarballs needed no changes and are byte-for-byte originals.

## Integrity notes

- The 0.5.1 SourceForge tarball is truncated (gzip reports "truncated input").
  Every file in its header list extracted intact before redaction. The real 0.5.1 changelog is lost:
  the file named `changelog` was the pre-release tarball above.
- Wayback files were fetched raw (the `id_` URL form), so no archive toolbar was injected.

## SHA-256

```
3b903717b73f9fa1ad99404730c9fe24c1f80edb6dfb769132ff7bc082d4f06c  0.1.2/porn-get
48acf40ab11960274d5bbfd9c94546cb65bb4a53ccced5b9602f5601b44b349f  0.1.2/porn-cache
3481649298d18a018ebd5119140423181fbd71eb65572701bbd1b11d2e741a2f  0.1.2/readme
b20fb499e7af0678557efa40040b1fc9d664e6ce5606fa0cc2573b589b263213  0.1.2/changelog
8fe3877cee0697f69e263743e43445a4f00f5f23fd7d475d8ded1e2ef40581b1  0.2.5/porn-get-0.2.5.tar.bz2
37c55a75e43bb457573944184a837a55a09bbb51dd2ea2be955bb869606a9868  0.4.0/porn-get-0.4.0.tar.bz2
e54fda1cf2be19a528c7439987015a1370af790d0676cd6201267eafeed154c6  site/index.html
```

## Sources

- https://web.archive.org/web/2002*/linuks.mine.nu/porn-get*
- https://web.archive.org/web/20020213224438/http://www.linuks.mine.nu:80/porn-get/old/porn-get-0.2.5.tar.bz2
- https://web.archive.org/web/20020202064746/http://www.linuks.mine.nu:80/porn-get/porn-get-0.4.0.tar.bz2
- https://sourceforge.net/projects/lesbianlinux/files/lesbianlinux/porn-get_0.5.1/

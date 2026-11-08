# Changelog

## 0.7.0

First public release. A Go rewrite of porn-get (2001-2003); see HISTORY.md.

- `search`, `show`, `install`, `remove`, `list`: apt-style commands for
  public videos.
- Every request goes through Tor: an existing proxy or a private tor that
  pron-get starts itself. It checks for a Tor exit first, sends no DNS
  outside Tor, and uses a fresh circuit for every video and every source.
- Downloads resume after an interruption. HLS segments are fetched in
  parallel, decrypted, and remuxed with ffmpeg. With `-j`, several videos
  download at once.
- Bulk install from model, pornstar, channel, user, playlist, category and
  search pages (newest 3 by default).
- `sources.list`, `update` and `upgrade`, with removed packages held back.
- `pron-cache` searches the package index offline.
- pron-get(1) man page.

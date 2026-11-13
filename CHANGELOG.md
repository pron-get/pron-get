# Changelog

## 0.7.6

- Page, playlist and Tor-check requests that stop receiving data for
  2 minutes are dropped and retried on a new circuit, instead of hanging
  forever.

## 0.7.5

- A download that stops receiving data for 60 s is dropped and tried
  again, instead of hanging forever. With `-x`, connections that run out
  of pieces wait for the rest, and take over any piece another connection
  gives up on.

## 0.7.4

0.7.3 was tagged but never released: its build failed on a flaky test.

- `install` and `upgrade` fetch each mp4 over 4 Tor circuits at once
  (`-x N`, `-x 1` for one). A 272 MB video took 133 s instead of about
  340 s. Interrupted downloads resume piece by piece.
- The download speed shown after a resume counts only new bytes.

## 0.7.2

- pron-get always starts its own tor unless `-tor` names a proxy. It no
  longer picks up a system tor or Tor Browser on 9050/9150, so closing Tor
  Browser can't stop a download.

## 0.7.1

- Homebrew formula: stop passing `-s -w` twice (`std_go_args` already
  adds them).

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

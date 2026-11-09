// pron-get: an apt-get style downloader for porn, over Tor.
//
// A tribute to porn-get (2001, Lesbian GNU/Linux). See HISTORY.md.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pron-get/pron-get/internal/fetch"
	"github.com/pron-get/pron-get/internal/index"
	"github.com/pron-get/pron-get/internal/pornhub"
	"github.com/pron-get/pron-get/internal/sources"
	"github.com/pron-get/pron-get/internal/store"
	"github.com/pron-get/pron-get/internal/tor"
)

// version picks up where porn-get 0.5.1 left off. Release builds set it with
// -ldflags "-X main.version=...".
var version = "0.7.0-dev"

const usage = `pron-get %s for everything, over Tor
usage: pron-get search <words...|listing-url>
       pron-get show <viewkey|url>
       pron-get install <viewkey|url|listing-url>... [-search words]
       pron-get remove <package>...
       pron-get update
       pron-get upgrade
       pron-get list [-upgradable]
       pron-get add-source <listing-url | search words> [limit=N]
       pron-get edit-sources
       pron-get cache search|show|pkgnames|stats
       pron-get torcheck

pron-get is a simple command line interface for downloading and installing
video packages. Every request goes through Tor; if Tor can't be reached,
nothing is sent.

Commands:
  search   - search for packages, or list a model/channel/playlist URL
  show     - show package details and available qualities
  install  - download packages (resumes interrupted downloads); takes
             videos, model/pornstar/channel/user/playlist URLs, or -search
  remove   - remove packages (upgrade won't bring them back)
  update   - fetch every source in sources.list into the package index
  upgrade  - install everything new in the package index
  list     - list installed (or -upgradable) packages
  add-source, edit-sources - manage sources.list
  cache    - search the package index offline (also: pron-cache)
  torcheck - show the Tor exit you would be using

Run "pron-get <command> -h" for options.
                       This porn has Super Cow Powers.
`

type opts struct {
	torAddr string
	torBin  string
	dir     string
	base    string
	onion   bool
	quality int
	hls     bool
	yes     bool
	page    int
	search  string
	limit   int
	jobs    int
	conns   int

	sourcesPath string
	indexPath   string
	upgradable  bool
}

func flags(name string, o *opts) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&o.torAddr, "tor", "", "use this Tor SOCKS proxy (default: start a private tor)")
	fs.StringVar(&o.torBin, "tor-bin", "tor", "tor binary to launch when -tor is not given")
	fs.StringVar(&o.dir, "dir", store.DefaultRoot(), "where packages are installed")
	fs.StringVar(&o.base, "site", pornhub.DefaultBase, "site base URL")
	fs.BoolVar(&o.onion, "onion", false, "use the site's onion service instead of a Tor exit")
	fs.StringVar(&o.sourcesPath, "sources", sources.DefaultPath(), "sources.list file")
	fs.StringVar(&o.indexPath, "index", index.DefaultPath(), "package index file")
	return fs
}

func main() {
	// Installed (or symlinked) as pron-cache, act like porn-cache did.
	if filepath.Base(os.Args[0]) == "pron-cache" {
		os.Args = append([]string{"pron-get", "cache"}, os.Args[1:]...)
	}
	if len(os.Args) < 2 {
		fmt.Printf(usage, version)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var o opts
	var err error
	switch cmd {
	case "search":
		fs := flags(cmd, &o)
		fs.IntVar(&o.page, "page", 1, "result page")
		fs.IntVar(&o.limit, "limit", 100, "with a listing URL: maximum results (0 = all)")
		fs.Parse(args)
		if fs.NArg() == 0 {
			fail("pron-get search <words...|listing-url>")
		}
		if l, ok := pornhub.ListingPath(fs.Arg(0)); ok && fs.NArg() == 1 {
			err = withSite(ctx, &o, func(s *session) error { return listing(ctx, s, l, o.limit) })
			break
		}
		err = withSite(ctx, &o, func(s *session) error { return search(ctx, s, strings.Join(fs.Args(), " "), o.page) })
	case "show":
		fs := flags(cmd, &o)
		fs.Parse(args)
		ids := parseIDs(fs.Args())
		if len(ids) != 1 {
			fail("pron-get show <viewkey|url>")
		}
		err = withSite(ctx, &o, func(s *session) error { return show(ctx, s, ids[0]) })
	case "install":
		fs := flags(cmd, &o)
		fs.IntVar(&o.quality, "q", 0, "maximum height, e.g. 720 (default: best)")
		fs.BoolVar(&o.hls, "hls", false, "prefer the HLS stream over the mp4 file")
		fs.StringVar(&o.search, "search", "", "install the results of this search")
		fs.IntVar(&o.limit, "limit", 3, "newest packages taken from each listing or search (0 = all)")
		fs.IntVar(&o.jobs, "j", 1, "parallel downloads, each on its own Tor circuit (max 8)")
		fs.IntVar(&o.conns, "x", 4, "connections per mp4 download, each on its own Tor circuit (max 8)")
		fs.BoolVar(&o.yes, "y", false, "don't ask for confirmation")
		fs.Parse(args)
		ids, listings := parseTargets(fs.Args())
		if o.search != "" {
			listings = append(listings, pornhub.SearchPath(o.search))
		}
		if len(ids)+len(listings) == 0 {
			fail("pron-get install <viewkey|video-url|listing-url>... [-search words]")
		}
		// Don't touch the network for packages we already have.
		st := store.Store{Root: o.dir}
		var todo []string
		for _, id := range ids {
			if st.Installed(id) {
				fmt.Printf("%s is already the newest version.\n", id)
			} else {
				todo = append(todo, id)
			}
		}
		if len(todo)+len(listings) == 0 {
			fmt.Println("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.")
			break
		}
		err = withSite(ctx, &o, func(s *session) error { return install(ctx, s, todo, listings, o) })
	case "remove":
		fs := flags(cmd, &o)
		fs.BoolVar(&o.yes, "y", false, "don't ask for confirmation")
		fs.Parse(args)
		if fs.NArg() == 0 {
			fmt.Println("pron-get remove <package>...")
			fmt.Println("0 packages upgraded, 0 newly installed, 0 to remove and 0 not upgraded.")
			os.Exit(1)
		}
		err = remove(store.Store{Root: o.dir}, fs.Args(), o.yes)
	case "update":
		fs := flags(cmd, &o)
		fs.Parse(args)
		srcs, lerr := loadSources(o.sourcesPath)
		if lerr != nil {
			err = lerr
			break
		}
		err = withSite(ctx, &o, func(s *session) error { return update(ctx, s, srcs, o.indexPath) })
	case "upgrade":
		fs := flags(cmd, &o)
		fs.IntVar(&o.quality, "q", 0, "maximum height, e.g. 720 (default: best)")
		fs.BoolVar(&o.hls, "hls", false, "prefer the HLS stream over the mp4 file")
		fs.IntVar(&o.limit, "limit", 0, "install at most this many (0 = all)")
		fs.IntVar(&o.jobs, "j", 1, "parallel downloads, each on its own Tor circuit (max 8)")
		fs.IntVar(&o.conns, "x", 4, "connections per mp4 download, each on its own Tor circuit (max 8)")
		fs.BoolVar(&o.yes, "y", false, "don't ask for confirmation")
		fs.Parse(args)
		st := store.Store{Root: o.dir}
		ids, info, perr := upgradePlan(st, o.indexPath, o.limit)
		if perr != nil {
			err = perr
			break
		}
		if len(ids) == 0 {
			fmt.Println("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.")
			break
		}
		// Ask before touching the network.
		if !o.yes && !confirm(ids, info) {
			fmt.Println("Abort.")
			break
		}
		err = withSite(ctx, &o, func(s *session) error { return installPkgs(ctx, s, ids, o) })
	case "add-source":
		fs := flags(cmd, &o)
		fs.Parse(args)
		if fs.NArg() == 0 {
			fail("pron-get add-source <listing-url | search words> [limit=N]")
		}
		err = addSource(o.sourcesPath, fs.Args())
	case "edit-sources":
		fs := flags(cmd, &o)
		fs.Parse(args)
		err = editSources(o.sourcesPath)
	case "cache":
		fs := flags(cmd, &o)
		fs.Parse(args)
		err = cache(fs.Args(), store.Store{Root: o.dir}, o.indexPath, o.sourcesPath)
	case "list":
		fs := flags(cmd, &o)
		fs.BoolVar(&o.upgradable, "upgradable", false, "list packages 'upgrade' would install")
		fs.Parse(args)
		if o.upgradable {
			err = listUpgradable(store.Store{Root: o.dir}, o.indexPath)
			break
		}
		err = list(store.Store{Root: o.dir})
	case "torcheck":
		fs := flags(cmd, &o)
		fs.Parse(args)
		err = withTor(ctx, &o, func(*tor.Client) error { return nil })
	case "moo":
		fmt.Print(cow)
	case "-v", "--version", "version":
		fmt.Println("pron-get", version)
	case "-h", "--help", "help":
		fmt.Printf(usage, version)
	default:
		fmt.Printf(usage, version)
		os.Exit(1)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "\nAbort. Partial downloads are kept and will resume.")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "E:", err)
		os.Exit(100) // apt's error code
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// parseTargets splits install arguments into single videos and listing pages
// (search results, models, pornstars, channels, users, playlists, categories).
func parseTargets(args []string) (ids, listings []string) {
	for _, a := range args {
		if id, ok := pornhub.ParseID(a); ok {
			ids = append(ids, id)
		} else if l, ok := pornhub.ListingPath(a); ok {
			listings = append(listings, l)
		} else {
			fail(fmt.Sprintf("E: Unable to locate package %s", a))
		}
	}
	return ids, listings
}

func parseIDs(args []string) []string {
	var ids []string
	for _, a := range args {
		id, ok := pornhub.ParseID(a)
		if !ok {
			fail(fmt.Sprintf("E: Unable to locate package %s", a))
		}
		ids = append(ids, id)
	}
	return ids
}

// withTor connects to Tor (a private instance, or the -tor proxy) and
// refuses to continue unless check.torproject.org confirms a Tor exit.
func withTor(ctx context.Context, o *opts, f func(*tor.Client) error) error {
	addr := o.torAddr
	if addr == "" {
		fmt.Fprintln(os.Stderr, "Starting private tor...")
		p, err := tor.Launch(ctx, o.torBin, tor.DefaultDataDir(), io.Discard)
		if err != nil {
			return err
		}
		defer p.Close()
		addr = p.Addr
	}
	c := tor.NewClient(addr)
	check, err := c.Verify(ctx)
	if err != nil {
		return fmt.Errorf("Tor check failed, refusing to continue: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Hit:1 tor %s exit %s\n", addr, check.IP)
	return f(c)
}

type session struct {
	tor  *tor.Client
	site *pornhub.Client
	st   store.Store
}

func withSite(ctx context.Context, o *opts, f func(*session) error) error {
	base := o.base
	if o.onion {
		base = pornhub.OnionBase
	}
	return withTor(ctx, o, func(c *tor.Client) error {
		return f(&session{
			tor:  c,
			site: &pornhub.Client{HTTP: c.Client, Base: base, NewCircuit: c.NewCircuit},
			st:   store.Store{Root: o.dir},
		})
	})
}

func search(ctx context.Context, s *session, q string, page int) error {
	rs, err := s.site.Search(ctx, q, page)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Reading Package Lists... Done")
	printResults(s, rs)
	return nil
}

func listing(ctx context.Context, s *session, path string, limit int) error {
	rs, err := s.site.List(ctx, path, limit, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Reading Package Lists... Done")
	printResults(s, rs)
	return nil
}

func printResults(s *session, rs []pornhub.Result) {
	if len(rs) == 0 {
		fmt.Println("No packages found")
		return
	}
	for _, r := range rs {
		mark := " "
		if s.st.Installed(r.ID) {
			mark = "i"
		}
		fmt.Printf("%s %-15s %8s  %s\n", mark, r.ID, r.Duration, r.Title)
	}
}

func show(ctx context.Context, s *session, id string) error {
	v, err := s.site.Video(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("Package: %s\n", v.ID)
	fmt.Printf("Title: %s\n", v.Title)
	if v.Uploader != "" {
		fmt.Printf("Maintainer: %s\n", v.Uploader)
	}
	if v.Duration > 0 {
		fmt.Printf("Duration: %s\n", time.Duration(v.Duration)*time.Second)
	}
	fmt.Printf("Homepage: %s\n", v.PageURL)
	fmt.Printf("Status: %s\n", map[bool]string{true: "installed", false: "not installed"}[s.st.Installed(v.ID)])
	var qs []string
	for _, f := range v.Formats {
		qs = append(qs, fmt.Sprintf("%dp/%s", f.Height, f.Kind))
	}
	fmt.Printf("Versions: %s\n", strings.Join(qs, " "))
	return nil
}

// expand turns listings into package names, appended after ids. Installed
// packages are skipped. titles collects what the listings said about each.
func expand(ctx context.Context, s *session, ids, listings []string, limit int) ([]string, map[string]pornhub.Result, error) {
	info := map[string]pornhub.Result{}
	seen := map[string]bool{}
	removed := s.st.Removed()
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, l := range listings {
		msg := "Reading package list " + l + "..."
		fmt.Fprint(os.Stderr, msg)
		rs, err := s.site.List(ctx, l, limit, func(page, n int) {
			fmt.Fprintf(os.Stderr, "\r%s page %d, %d packages", msg, page, n)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr)
			return nil, nil, err
		}
		fmt.Fprintln(os.Stderr, " Done")
		for _, r := range rs {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if s.st.Installed(r.ID) {
				fmt.Printf("%s is already the newest version.\n", r.ID)
				continue
			}
			if removed[r.ID] {
				fmt.Printf("%s was removed; skipping (install it by name to bring it back).\n", r.ID)
				continue
			}
			info[r.ID] = r
			out = append(out, r.ID)
		}
	}
	return out, info, nil
}

// confirm prints apt's "NEW packages" block and asks to continue.
func confirm(ids []string, info map[string]pornhub.Result) bool {
	fmt.Println("The following NEW packages will be installed:")
	var total time.Duration
	for _, id := range ids {
		r := info[id]
		fmt.Printf("  %-15s %8s  %s\n", id, r.Duration, r.Title)
		total += parseClock(r.Duration)
	}
	fmt.Printf("0 upgraded, %d newly installed, 0 to remove and 0 not upgraded.\n", len(ids))
	if total > 0 {
		fmt.Printf("After this operation, %s of video will be downloaded.\n", total)
	}
	fmt.Print("Do you want to continue? [Y/n] ")
	reply, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	r := strings.ToLower(strings.TrimSpace(reply))
	return r == "" || r == "y" || r == "yes"
}

// parseClock reads "1:02:03" or "12:34".
func parseClock(s string) time.Duration {
	var d time.Duration
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0
		}
		d = d*60 + time.Duration(n)
	}
	return d * time.Second
}

// worker downloads on its own Tor client, so parallel jobs never share a
// circuit (or an exit IP) with each other.
type worker struct {
	tor  *tor.Client
	site *pornhub.Client
	st   store.Store
	bar  bool // draw a progress bar (only when running one job)
}

func install(ctx context.Context, s *session, ids, listings []string, o opts) error {
	ids, info, err := expand(ctx, s, ids, listings, o.limit)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.")
		return nil
	}
	if len(listings) > 0 && !o.yes && !confirm(ids, info) {
		fmt.Println("Abort.")
		return nil
	}
	// Installing by hand undoes an earlier remove.
	if err := s.st.SetRemoved(false, ids...); err != nil {
		return err
	}
	return installPkgs(ctx, s, ids, o)
}

// installPkgs downloads ids with o.jobs parallel workers.
func installPkgs(ctx context.Context, s *session, ids []string, o opts) error {
	jobs := min(max(o.jobs, 1), 8, len(ids))
	type job struct {
		n  int
		id string
	}
	queue := make(chan job)
	var (
		mu                sync.Mutex
		installed, failed int
		wg                sync.WaitGroup
	)
	for w := 0; w < jobs; w++ {
		wk := worker{tor: s.tor, st: s.st, bar: jobs == 1}
		if w > 0 {
			wk.tor = s.tor.Fork()
		}
		wk.site = &pornhub.Client{HTTP: wk.tor.Client, Base: s.site.Base, NewCircuit: wk.tor.NewCircuit}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				// A fresh circuit per video, so the site can't link them by exit IP.
				wk.tor.NewCircuit()
				err := installOne(ctx, wk, j.n, j.id, o)
				mu.Lock()
				if err == nil {
					installed++
				} else if ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "E: %s: %v\n", j.id, err)
					failed++
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for n, id := range ids {
		select {
		case queue <- job{n + 1, id}:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	fmt.Printf("0 upgraded, %d newly installed, 0 to remove and %d not upgraded.\n", installed, failed)
	if failed > 0 {
		return fmt.Errorf("%d package(s) failed", failed)
	}
	return nil
}

func installOne(ctx context.Context, s worker, n int, id string, o opts) error {
	var v *pornhub.Video
	var f pornhub.Format
	resolve := func() error {
		var err error
		if v, err = s.site.Video(ctx, id); err != nil {
			return err
		}
		fs := v.Formats
		if o.hls {
			fs = prefer(fs, "hls")
		}
		var ok bool
		if f, ok = pornhub.Pick(fs, o.quality); !ok {
			return pornhub.ErrNoFormats
		}
		return nil
	}
	if err := resolve(); err != nil {
		return err
	}

	dir, err := s.st.Prepare(id)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, store.Slug(v.Title)+".mp4")
	fmt.Printf("Get:%d %s %dp/%s %s\n", n, id, f.Height, f.Kind, v.Title)
	bar := newProgress(f.Kind, s.bar)

	var out string
	for attempt := 1; ; attempt++ {
		if f.Kind == "hls" {
			out, err = fetch.HLS(ctx, s.tor.Client, f.URL, dst, o.quality, s.site.Headers(), bar.update)
		} else if o.conns > 1 {
			first := fetch.Source{Client: s.tor.Client, URL: f.URL}
			out, err = dst, fetch.Parallel(ctx, first, s.resolver(id, f), min(o.conns, 8), dst, s.site.Headers(), bar.update)
		} else {
			out, err = dst, fetch.File(ctx, s.tor.Client, f.URL, dst, s.site.Headers(), bar.update)
		}
		if err == nil || attempt == 3 || ctx.Err() != nil {
			break
		}
		// Media links are signed for one IP and expire. Resolve again on a
		// fresh circuit and resume from the .part file.
		bar.done()
		fmt.Fprintf(os.Stderr, "W: %v; retrying on a new circuit\n", err)
		s.tor.NewCircuit()
		if rerr := resolve(); rerr != nil {
			return rerr
		}
	}
	bar.done()
	if err != nil {
		return err
	}

	sum, size, err := hashFile(out)
	if err != nil {
		return err
	}
	fmt.Printf("Setting up %s (%s) ...\n", id, humanBytes(size))
	return s.st.Save(store.Package{
		Name:        id,
		Title:       v.Title,
		Uploader:    v.Uploader,
		Duration:    v.Duration,
		Source:      v.PageURL,
		Height:      f.Height,
		File:        filepath.Base(out),
		Size:        size,
		SHA256:      sum,
		InstalledAt: time.Now().UTC(),
	})
}

// resolver gives each extra connection its own circuit and its own media URL,
// since the CDN signs URLs for a single exit IP.
func (s worker) resolver(id string, want pornhub.Format) fetch.Resolver {
	return func(ctx context.Context) (fetch.Source, error) {
		c := s.tor.Fork()
		site := &pornhub.Client{HTTP: c.Client, Base: s.site.Base, NewCircuit: c.NewCircuit}
		v, err := site.Video(ctx, id)
		if err != nil {
			return fetch.Source{}, err
		}
		for _, f := range v.Formats {
			if f.Kind == want.Kind && f.Height == want.Height {
				return fetch.Source{Client: c.Client, URL: f.URL}, nil
			}
		}
		return fetch.Source{}, fmt.Errorf("%dp %s not offered on a new circuit", want.Height, want.Kind)
	}
}

// prefer moves formats of the given kind to the front, keeping order.
func prefer(fs []pornhub.Format, kind string) []pornhub.Format {
	var a, b []pornhub.Format
	for _, f := range fs {
		if f.Kind == kind {
			a = append(a, f)
		} else {
			b = append(b, f)
		}
	}
	return append(a, b...)
}

func remove(st store.Store, names []string, yes bool) error {
	var freed int64
	for _, n := range names {
		if !st.Installed(n) {
			if _, err := st.Dir(n); err != nil {
				return err
			}
			return fmt.Errorf("Couldn't find package %s", n)
		}
		freed += st.DirSize(n)
	}
	fmt.Println("The following packages will be REMOVED:")
	fmt.Println("  " + strings.Join(names, " "))
	fmt.Printf("0 upgraded, 0 newly installed, %d to remove and 0 not upgraded.\n", len(names))
	fmt.Printf("After this operation, %s disk space will be freed.\n", humanBytes(freed))
	if !yes {
		fmt.Print("Do you want to continue? [y/N] ")
		reply, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if r := strings.ToLower(strings.TrimSpace(reply)); r != "y" && r != "yes" {
			fmt.Println("Abort.")
			return nil
		}
	}
	for _, n := range names {
		fmt.Printf("Removing %s ...\n", n)
		if err := st.Remove(n); err != nil {
			return err
		}
		if err := st.SetRemoved(true, n); err != nil {
			return err
		}
	}
	return nil
}

func listUpgradable(st store.Store, ixPath string) error {
	ids, info, err := upgradePlan(st, ixPath, 0)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Listing... Done")
	for _, id := range ids {
		r := info[id]
		fmt.Printf("%-15s %8s  %s [upgradable]\n", id, r.Duration, r.Title)
	}
	return nil
}

func list(st store.Store) error {
	ps, err := st.List()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Listing... Done")
	for _, p := range ps {
		fmt.Printf("%-15s %5dp %8s  %s\n", p.Name, p.Height, humanBytes(p.Size), p.Title)
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

type progress struct {
	kind    string
	start   time.Time
	base    int64 // bytes already on disk when the download (re)started
	enabled bool
	shown   bool
}

func newProgress(kind string, enabled bool) *progress {
	return &progress{kind: kind, start: time.Now(), enabled: enabled}
}

func (p *progress) update(done, total int64) {
	if !p.enabled {
		return
	}
	if !p.shown {
		p.base, p.start = done, time.Now()
	}
	p.shown = true
	if p.kind == "hls" {
		fmt.Fprintf(os.Stderr, "\r  %3d%% [segment %d/%d]   ", done*100/max(total, 1), done, total)
		return
	}
	rate := float64(done-p.base) / max(time.Since(p.start).Seconds(), 0.001)
	if total > 0 {
		fmt.Fprintf(os.Stderr, "\r  %3d%% [%s/%s] %s/s   ", done*100/total, humanBytes(done), humanBytes(total), humanBytes(int64(rate)))
	} else {
		fmt.Fprintf(os.Stderr, "\r  [%s] %s/s   ", humanBytes(done), humanBytes(int64(rate)))
	}
}

func (p *progress) done() {
	if p.shown {
		fmt.Fprintln(os.Stderr)
		p.shown = false
	}
}

const cow = `         (__)
         (oo)
   /------\/
  / |    ||
 *  /\---/\
    ~~   ~~
...."Have you mooed today?"...
`

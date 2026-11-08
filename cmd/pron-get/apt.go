package main

// The sources.list → update → upgrade flow, and pron-cache.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pron-get/pron-get/internal/index"
	"github.com/pron-get/pron-get/internal/pornhub"
	"github.com/pron-get/pron-get/internal/sources"
	"github.com/pron-get/pron-get/internal/store"
)

func loadSources(path string) ([]sources.Source, error) {
	srcs, err := sources.Load(path)
	if err != nil {
		return nil, err
	}
	if len(srcs) == 0 {
		return nil, fmt.Errorf("The list of sources could not be read: %s has no sources.\n"+
			"   Add one with: pron-get add-source <listing-url | search words>", path)
	}
	return srcs, nil
}

// update fetches every source and rebuilds the package index.
func update(ctx context.Context, s *session, srcs []sources.Source, ixPath string) error {
	old, err := index.Load(ixPath)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, e := range old.Entries {
		known[e.ID] = true
	}

	start := time.Now()
	var fetched []index.Fetched
	total, failed := 0, 0
	for i, src := range srcs {
		// Each source on its own circuit, so the site can't tie your
		// sources together by exit IP.
		s.tor.NewCircuit()
		rs, err := s.site.List(ctx, src.Path, src.Limit, nil)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n := i + 2 // Hit:1 was the tor check
		if err != nil {
			fmt.Printf("Err:%d %s\n  %v\n", n, src.Path, err)
			failed++
		} else {
			verb := "Hit"
			for _, r := range rs {
				if !known[r.ID] {
					verb = "Get"
					break
				}
			}
			fmt.Printf("%s:%d %s [%d packages]\n", verb, n, src.Path, len(rs))
			total += len(rs)
		}
		fetched = append(fetched, index.Fetched{Path: src.Path, Results: rs, Err: err})
	}

	ix, added := index.Rebuild(old, fetched, time.Now().UTC())
	if err := ix.Save(ixPath); err != nil {
		return err
	}
	fmt.Printf("Fetched %d packages in %s\n", total, time.Since(start).Round(time.Second))
	fmt.Println("Reading Package Lists... Done")
	fmt.Println("Building dependency tree... Done")
	if up := upgradable(s.st, ix); len(up) > 0 {
		fmt.Printf("%d packages can be upgraded (%d new since last update). Run 'pron-get upgrade' to see them.\n", len(up), added)
	} else {
		fmt.Println("All packages are up to date.")
	}
	if failed > 0 {
		fmt.Println("W: Some index files failed to download. They have been ignored, or old ones used instead.")
	}
	return nil
}

// upgradable lists indexed packages that are neither installed nor removed.
func upgradable(st store.Store, ix *index.Index) []index.Entry {
	removed := st.Removed()
	var out []index.Entry
	for _, e := range ix.Entries {
		if !removed[e.ID] && !st.Installed(e.ID) {
			out = append(out, e)
		}
	}
	return out
}

func loadIndex(path string) (*index.Index, error) {
	ix, err := index.Load(path)
	if err != nil {
		return nil, err
	}
	if ix.Updated.IsZero() {
		return nil, errors.New("no package index yet; run 'pron-get update' first")
	}
	return ix, nil
}

// upgradePlan works out what upgrade would install, without the network.
func upgradePlan(st store.Store, ixPath string, limit int) ([]string, map[string]pornhub.Result, error) {
	ix, err := loadIndex(ixPath)
	if err != nil {
		return nil, nil, err
	}
	up := upgradable(st, ix)
	if limit > 0 && len(up) > limit {
		up = up[:limit]
	}
	ids := make([]string, len(up))
	info := map[string]pornhub.Result{}
	for i, e := range up {
		ids[i] = e.ID
		info[e.ID] = pornhub.Result{ID: e.ID, Title: e.Title, Duration: e.Duration}
	}
	return ids, info, nil
}

func addSource(path string, args []string) error {
	src, err := sources.Add(path, strings.Join(args, " "))
	if err != nil {
		return err
	}
	fmt.Printf("Adding source %s (%s, limit %d) to %s\n", src.Raw, src.Path, src.Limit, path)
	fmt.Println("Run 'pron-get update' to fetch it.")
	return nil
}

func editSources(path string) error {
	if _, err := sources.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "W:", err)
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	argv := append(strings.Fields(editor), path)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	srcs, err := sources.Load(path)
	if err != nil {
		return err
	}
	fmt.Printf("%d sources. Run 'pron-get update' to fetch them.\n", len(srcs))
	return nil
}

const cacheUsage = `pron-cache %s (pron-get cache)
usage: pron-cache search <regex>
       pron-cache show <package>
       pron-cache pkgnames
       pron-cache stats

Works offline on the index built by 'pron-get update'.
`

// cache is porn-cache: offline queries against the package index.
func cache(args []string, st store.Store, ixPath, srcPath string) error {
	if len(args) == 0 {
		fmt.Printf(cacheUsage, version)
		return nil
	}
	ix, err := loadIndex(ixPath)
	if err != nil {
		return err
	}
	mark := func(id string) string {
		if st.Installed(id) {
			return "i"
		}
		if st.Removed()[id] {
			return "r"
		}
		return " "
	}
	switch args[0] {
	case "search":
		if len(args) < 2 {
			return errors.New("pron-cache search <regex>")
		}
		es, err := ix.Search(strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		for _, e := range es {
			fmt.Printf("%s %-15s %8s  %s\n", mark(e.ID), e.ID, e.Duration, e.Title)
		}
	case "show":
		if len(args) != 2 {
			return errors.New("pron-cache show <package>")
		}
		e, ok := ix.Get(args[1])
		if !ok {
			return fmt.Errorf("Unable to locate package %s", args[1])
		}
		fmt.Printf("Package: %s\nTitle: %s\nDuration: %s\nSources: %s\nFirst-Seen: %s\n",
			e.ID, e.Title, e.Duration, strings.Join(e.Sources, " "), e.FirstSeen.Format(time.DateTime))
	case "pkgnames":
		for _, e := range ix.Entries {
			fmt.Println(e.ID)
		}
	case "stats":
		installed := 0
		for _, e := range ix.Entries {
			if st.Installed(e.ID) {
				installed++
			}
		}
		srcs, _ := sources.Load(srcPath)
		fmt.Printf("Total package names: %d\n", len(ix.Entries))
		fmt.Printf("  Installed: %d\n", installed)
		fmt.Printf("  Upgradable: %d\n", len(upgradable(st, ix)))
		fmt.Printf("  Removed (held back): %d\n", len(st.Removed()))
		fmt.Printf("Sources: %d (%s)\n", len(srcs), srcPath)
		fmt.Printf("Last update: %s ago\n", time.Since(ix.Updated).Round(time.Minute))
	default:
		fmt.Printf(cacheUsage, version)
		return fmt.Errorf("unknown cache command %q", args[0])
	}
	return nil
}
